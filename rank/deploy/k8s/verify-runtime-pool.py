"""Exercise CPU and memory admission across two Workers and real remote DSH containers."""
from datetime import datetime, timezone
import json
import socket
import subprocess
import time
import urllib.error
import urllib.request
import uuid

from lab import HERE, KUBE, STATE, run
from verify import kubectl, wait


def main():
    if json.loads((STATE / 'platform-mode.json').read_text())['executor'] != 'opensandbox':
        raise RuntimeError('Install the complete remote platform first')
    if kubectl('get', 'scaledobject/execution-worker', '--ignore-not-found', '-o', 'name').strip():
        raise RuntimeError('Disable autoscaling before this experiment')
    deployment = json.loads(kubectl('get', 'deployment/execution-worker', '-o', 'json'))
    if deployment['spec']['replicas'] != 2:
        raise RuntimeError('Use the two-worker lab configuration')
    args = deployment['spec']['template']['spec']['containers'][0]['args']
    flags = dict(arg[1:].split('=', 1) for arg in args if '=' in arg)
    if flags.get('sandbox-pool') != 'rank-dev' or flags.get('sandbox-scope') != 'rank-dev':
        raise RuntimeError('Only the dedicated rank-dev pool is supported')
    token = json.loads((STATE / 'execution-lab.json').read_text())['EXECUTION_SERVICE_TOKEN']
    images = json.loads((STATE / 'platform-images.json').read_text())
    prefix = 'resource-' + uuid.uuid4().hex
    container = 'rank-' + prefix
    report = {'startedAt': datetime.now(timezone.utc).isoformat(), 'images': images, 'phases': [], 'executions': []}
    forward = None
    fixture_created = False
    pool_changed = False
    base = ''

    def request(path, body=None):
        req = urllib.request.Request(base + path, headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'}, data=None if body is None else json.dumps(body).encode())
        with urllib.request.urlopen(req, timeout=30) as response:
            return json.load(response)

    def configure(cpu, memory):
        kubectl('exec', 'deployment/execution-api', '--', '/usr/local/bin/executiond', '-executor=opensandbox', '-sandbox-pool=rank-dev', '-sandbox-scope=rank-dev', '-sandbox-capacity-cpu=' + cpu, '-sandbox-capacity-memory=' + memory, '-configure-sandbox-pool')

    def outcomes(identifiers):
        return [request('/v1/executions/' + identifier) for identifier in identifiers]

    def settled(values):
        return all(value['status'] in ['completed', 'failed', 'cancelled'] and not value.get('runtimeCleanupPending') for value in values)

    def containers():
        return [name for name in run(['docker', 'ps', '--format', '{{.Names}}'], capture=True).splitlines() if name.startswith('sandbox-')]

    try:
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0)); port = listener.getsockname()[1]
        forward = subprocess.Popen(KUBE + ['-n', 'rank-dev', 'port-forward', '--address=127.0.0.1', 'service/execution-api', str(port) + ':8790'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        base = 'http://127.0.0.1:' + str(port)
        def healthy():
            try:
                with urllib.request.urlopen(base + '/v1/health', timeout=2) as response:
                    return response.status == 200
            except (urllib.error.URLError, OSError):
                return False
        wait(healthy, 30)
        capacity = request('/v1/capacity')
        if any(capacity[q][key] for q in ['online', 'evaluation'] for key in ['active', 'ready', 'resourceBlocked', 'quotaBlocked', 'capabilityBlocked', 'deliveryBlocked', 'expired', 'cleanupPending']) or containers():
            raise RuntimeError('Run this verifier on an idle lab without other sandboxes')
        run(['docker', 'run', '-d', '--name', container, '--label', 'rank.lab=' + prefix, '--network', 'rank-dev', '--cpus=0.5', '--memory=128m', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--user=1000:1000', '-e', 'RANK_LAB_DELAY_MS=10000', '--entrypoint', 'node', images['images']['platform'], '/opt/rank/fixtures/model.mjs'], capture=True)
        fixture_created = True
        model_ip = run(['docker', 'inspect', container, '--format', '{{(index .NetworkSettings.Networks "rank-dev").IPAddress}}'], capture=True).strip()
        connections = []
        for index in range(3):
            connection = request('/v1/model-connections', {'name': prefix + str(index), 'provider': 'custom', 'protocol': 'openai-chat-completions', 'baseUrl': 'http://' + model_ip + ':8800/v1', 'apiKey': 'rank-lab-fixture', 'defaultModel': 'rank-lab'})
            # Model discovery can be retried without submitting an invocation.
            def verified():
                try:
                    request('/v1/model-connections/' + connection['id'] + '/verify', {}); return True
                except urllib.error.HTTPError:
                    return False
            wait(verified, 20)
            connections.append(connection['id'])
        for label, cpu, memory in [('cpu', '500m', '2Gi'), ('memory', '2', '512Mi')]:
            pool_changed = True
            configure(cpu, memory)
            phase = {'name': label, 'capacityCPU': cpu, 'capacityMemory': memory, 'maxContainers': 0, 'blockedDemandExcluded': False, 'blockedAttemptsZero': False, 'samples': []}
            report['phases'].append(phase)
            identifiers = []
            for index, connection in enumerate(connections):
                key = prefix + '-' + label + '-' + str(index)
                value = request('/v1/executions', {'idempotencyKey': key, 'spec': {'protocolVersion': 2, 'kind': 'agent', 'harness': 'dsh', 'prompt': 'Acknowledge the resource admission experiment.', 'modelConnectionId': connection, 'model': 'rank-lab', 'timeoutMs': 60000, 'metadata': {'responseId': key, 'tenantId': key}}})
                identifiers.append(value['id']); report['executions'].append(value['id'])
                assert value['runtimePlan']['resources'] == {'cpuMillis': 500, 'memoryBytes': 536870912}
            print('Resource phase:', label, 'waiting for three distinct tenants/models', flush=True)
            deadline = time.monotonic() + 100
            while True:
                capacity = request('/v1/capacity')['online']
                values = outcomes(identifiers)
                names = containers()
                phase['maxContainers'] = max(phase['maxContainers'], len(names))
                assert len(names) <= 1, 'Sandbox host budget overcommitted'
                phase['samples'].append({key: capacity[key] for key in ['active', 'ready', 'resourceBlocked', 'quotaBlocked', 'demand']})
                if capacity['resourceBlocked'] >= 1:
                    assert capacity['active'] + capacity['ready'] <= 1 and capacity['quotaBlocked'] == 0 and capacity['demand'] <= 1
                    phase['blockedDemandExcluded'] = True
                    queued = [value for value in values if value['status'] == 'queued']
                    if queued:
                        assert all(value['attempt'] == 0 and not value.get('runtime') for value in queued)
                        phase['blockedAttemptsZero'] = True
                for name in names:
                    inspected = subprocess.run(['docker', 'inspect', name, '--format', '{{json .HostConfig}}'], capture_output=True, text=True)
                    if inspected.returncode:
                        if 'No such object' in inspected.stderr:
                            continue
                        raise RuntimeError('Could not inspect owned runtime limits')
                    limits = json.loads(inspected.stdout)
                    assert limits['NanoCpus'] == 500_000_000 and limits['Memory'] == 536870912
                if settled(values):
                    phase['outcomes'] = [{'status': value['status'], 'attempt': value['attempt'], 'runtimeCleanupPending': bool(value.get('runtimeCleanupPending'))} for value in values]
                    assert all(value['status'] == 'completed' and value['attempt'] == 1 for value in values)
                    break
                if time.monotonic() > deadline:
                    raise TimeoutError('Resource-limited work did not drain')
                time.sleep(0.5)
            assert phase['maxContainers'] == 1 and phase['blockedDemandExcluded'] and phase['blockedAttemptsZero']
        snapshot = [{'name': p['name'], 'maxContainers': p['maxContainers'], 'blockedDemandExcluded': p['blockedDemandExcluded'], 'blockedAttemptsZero': p['blockedAttemptsZero'], 'outcomes': p['outcomes']} for p in report['phases']]
        expected = json.loads((HERE / 'fixtures/runtime-pool.snapshot.json').read_text())
        assert snapshot == expected, 'Runtime pool transcript differs'
        report['snapshot'] = snapshot
        report['passed'] = True
    except Exception as error:
        report['failure'] = type(error).__name__
        raise
    finally:
        cleanup_errors = []
        def cleanup(label, action):
            try:
                action()
            except Exception as error:
                cleanup_errors.append({'action': label, 'error': type(error).__name__})
        def settle_submitted():
            if report['executions']:
                for value in outcomes(report['executions']):
                    if value['status'] not in ['completed', 'failed', 'cancelled']:
                        request('/v1/executions/' + value['id'] + '/cancel', {})
                wait(lambda: settled(outcomes(report['executions'])), 100)
        cleanup('settle submitted work', settle_submitted)
        if pool_changed:
            cleanup('restore durable pool budget', lambda: configure(flags['sandbox-capacity-cpu'], flags['sandbox-capacity-memory']))
        if fixture_created:
            cleanup('remove model fixture', lambda: run(['docker', 'rm', '-f', container], capture=True))
        if forward is not None:
            forward.terminate()
            cleanup('stop port forward', lambda: forward.wait(timeout=10))
        report['endedAt'] = datetime.now(timezone.utc).isoformat()
        if cleanup_errors:
            report['cleanupErrors'] = cleanup_errors
            report['passed'] = False
        path = STATE / (prefix + '.json')
        path.write_text(json.dumps(report, indent=2) + '\n')
        print('Runtime pool evidence:', path, flush=True)
        if cleanup_errors:
            raise RuntimeError('Lab cleanup needs inspection; see the retained report')



if __name__ == '__main__':
    main()
