"""Kill one identified lab Worker and verify its actual container exit."""
import json
from lab import run
from verify import kubectl, wait


def kill_owner(value):
    pods = json.loads(kubectl('get', 'pods', '-l', 'app=execution-worker', '-o', 'json'))['items']
    owner = next(pod for pod in pods if value['externalHandle'].startswith(pod['metadata']['name'] + '-'))
    if owner['spec']['nodeName'] != 'rank-dev':
        raise RuntimeError('This verifier only controls the dedicated rank-dev node')
    status = next(item for item in owner['status']['containerStatuses'] if item['name'] == 'worker')
    runtime, identifier = status['containerID'].split('://', 1)
    if runtime != 'containerd' or len(identifier) != 64:
        raise RuntimeError('Expected a concrete containerd worker identity')
    inspected = json.loads(run(['docker', 'exec', 'rank-dev', 'crictl', 'inspect', identifier], capture=True))
    pid = inspected.get('info', {}).get('pid')
    if inspected['status']['state'] != 'CONTAINER_RUNNING' or not isinstance(pid, int) or pid < 2:
        raise RuntimeError('Worker is no longer a running container with a known PID')
    # SIGKILL from the node namespace bypasses graceful shutdown. Recheck
    # cgroup ownership before signalling; deleting the Pod object is not proof.
    command = 'case "$(cat /proc/$1/cgroup)" in *"$2"*) kill -KILL "$1";; *) exit 2;; esac'
    run(['docker', 'exec', 'rank-dev', 'sh', '-c', command, 'worker-fault', str(pid), identifier], capture=True)
    def stopped():
        value = json.loads(run(['docker', 'exec', 'rank-dev', 'crictl', 'inspect', identifier], capture=True))['status']
        return value if value['state'] == 'CONTAINER_EXITED' else None
    stopped_status = wait(stopped, 15)
    if stopped_status.get('exitCode') != 137:
        raise AssertionError('The worker did not exit from SIGKILL')
    return {'pod': owner['metadata']['name'], 'containerId': identifier, 'state': stopped_status['state'], 'exitCode': stopped_status.get('exitCode'), 'finishedAt': stopped_status.get('finishedAt')}
