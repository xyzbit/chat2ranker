"""Verify live Worker scale-down and rolling replacement through real DSH tools."""
from datetime import datetime, timezone
import hashlib
import importlib.util
import sys
import json
import socket
import subprocess
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

from lab import HERE, KUBE, STATE, run
from verify import kubectl, wait


class PodWatch:
    """Retain terminal container facts before Kubernetes deletes the Pod object."""
    def __init__(self):
        columns = 'custom-columns=NAME:.metadata.name,UID:.metadata.uid,DELETING:.metadata.deletionTimestamp,EXIT:.status.containerStatuses[0].state.terminated.exitCode,PHASE:.status.phase'
        self.process = subprocess.Popen(KUBE + ['-n', 'rank-dev', 'get', 'pods', '-l', 'app=execution-worker', '--watch', '--no-headers', '-o', columns], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        self.values = {}
        self.lock = threading.Lock()
        self.thread = threading.Thread(target=self.read, daemon=True)
        self.thread.start()

    def read(self):
        for line in self.process.stdout:
            fields = line.split()
            if len(fields) != 5:
                continue
            name, uid, deleting, exit_code, phase = fields
            with self.lock:
                value = self.values.setdefault(uid, {'name': name, 'uid': uid})
                if deleting != '<none>':
                    value['deletingAt'] = deleting
                    value.setdefault('deletionObservedAt', time.monotonic())
                if exit_code != '<none>':
                    value.update(exitCode=int(exit_code), phase=phase)
                    value.setdefault('exitObservedAt', time.monotonic())

    def get(self, uid):
        if self.process.poll() is not None:
            raise RuntimeError('Worker Pod watch stopped before verification completed')
        with self.lock:
            return dict(self.values.get(uid, {}))

    def close(self):
        self.process.terminate()
        self.process.wait(timeout=10)
        self.thread.join(timeout=5)
        self.process.stdout.close()
        if self.thread.is_alive():
            raise RuntimeError('Worker Pod watch did not stop')


def main():
    automatic = '--keda' in sys.argv
    spec = importlib.util.spec_from_file_location('autoscaling_lab', HERE / 'autoscaling-lab.py')
    scaling = importlib.util.module_from_spec(spec); spec.loader.exec_module(scaling)
    scaler_enabled = False
    if json.loads((STATE / 'platform-mode.json').read_text())['executor'] != 'opensandbox':
        raise RuntimeError('Use the complete remote-runtime platform')
    if kubectl('get', 'scaledobject/execution-worker', '--ignore-not-found', '-o', 'name').strip():
        raise RuntimeError('Disable autoscaling before the active drain experiment')
    deployment = json.loads(kubectl('get', 'deployment/execution-worker', '-o', 'json'))
    template = deployment['spec']['template']['spec']
    args = template['containers'][0]['args']
    if deployment['spec']['replicas'] != 2 or '-online-slots=1' not in args or '-shutdown-grace=15s' not in args or template['terminationGracePeriodSeconds'] != 25:
        raise RuntimeError('This scenario requires two Workers, one online slot each, 15-second drain and 25-second Pod grace')
    original_annotations = deployment['spec']['template'].get('metadata', {}).get('annotations', {})
    images = json.loads((STATE / 'platform-images.json').read_text())
    token = json.loads((STATE / 'execution-lab.json').read_text())['EXECUTION_SERVICE_TOKEN']
    prefix = 'fault-' + uuid.uuid4().hex
    container = 'rank-' + prefix
    report = {'startedAt': datetime.now(timezone.utc).isoformat(), 'images': images, 'workerConfig': {'args': args, 'podGraceSeconds': 25}, 'scenarios': [], 'submittedExecutionIds': []}
    forward = watch = None
    created_model = mutated_workers = False
    base = model = ''

    def http(origin, path, body=None, auth=False, raw=False):
        headers = {'Content-Type': 'application/json'}
        if auth:
            headers['Authorization'] = 'Bearer ' + token
        request = urllib.request.Request(origin + path, headers=headers, data=None if body is None else json.dumps(body).encode())
        with urllib.request.urlopen(request, timeout=10) as response:
            return response.read().decode() if raw else json.load(response)

    def request(path, body=None, raw=False):
        return http(base, path, body, auth=True, raw=raw)

    def healthy(origin):
        try:
            return http(origin, '/v1/health' if origin == base else '/health')
        except (urllib.error.URLError, OSError):
            return False

    def worker_pods():
        return json.loads(kubectl('get', 'pods', '-l', 'app=execution-worker', '-o', 'json'))['items']

    def get(identifier):
        return request('/v1/executions/' + identifier)

    def settled(identifier):
        value = get(identifier)
        return value if value['status'] in ['completed', 'failed', 'cancelled'] and not value.get('runtimeCleanupPending') else None

    def submit(label):
        key = prefix + '-' + label
        body = {'idempotencyKey': key, 'spec': {'protocolVersion': 2, 'kind': 'agent', 'harness': 'dsh', 'prompt': key, 'modelConnectionId': connection['id'], 'model': 'rank-lab', 'timeoutMs': 60000, 'metadata': {'responseId': key, 'tenantId': prefix}}}
        value = request('/v1/executions', body)
        report['submittedExecutionIds'].append(value['id'])
        return {'id': value['id'], 'key': key, 'body': body}

    def held(item):
        value = get(item['id'])
        if value['status'] in ['completed', 'failed', 'cancelled']:
            raise AssertionError('Execution ended before its tool could be held')
        facts = http(model, '/facts').get(item['key'], {})
        return value if facts.get('effects') == 1 and value.get('runtime', {}).get('handle') else None

    def owner(value, pods):
        matches = [pod for pod in pods if value['externalHandle'].startswith(pod['metadata']['name'] + '-')]
        if len(matches) != 1:
            raise AssertionError('Execution owner does not identify one current Worker Pod')
        return matches[0]['metadata']

    def release(item):
        http(model, '/release?key=' + item['key'], {})

    def exit_fact(item):
        fact = watch.get(item['owner']['uid'])
        return fact if 'exitCode' in fact else None

    def check_item(item, expected):
        value = wait(lambda: settled(item['id']), 90)
        assert value['status'] == expected and value['attempt'] == 1
        handle = value['runtime']['handle']
        inspected = subprocess.run(['docker', 'inspect', 'sandbox-' + handle, '--format', '{{.State.Running}}'], capture_output=True, text=True)
        assert inspected.returncode and 'No such object' in inspected.stderr
        replay = request('/v1/executions', item['body'])
        assert (replay['id'], replay['status'], replay['attempt']) == (value['id'], expected, 1)
        events = [json.loads(line[6:]) for line in request('/v1/executions/' + value['id'] + '/events', raw=True).splitlines() if line.startswith('data: ')]
        assert sum(event['type'] == 'execution.running' for event in events) == 1
        assert sum(event['type'] in ['execution.completed', 'execution.failed', 'execution.cancelled'] for event in events) == 1
        facts = http(model, '/facts')[item['key']]
        if '-tool-' in item['key']:
            assert facts['effects'] == facts['closedEffects'] == facts['deniedWrites'] == 1
        if expected == 'completed':
            ref = next(ref for ref in value['result']['artifacts'] if ref['kind'] == 'stdout')
            artifact = request('/v1/executions/' + value['id'] + '/artifacts?path=' + urllib.parse.quote(ref['path']))
            assert artifact['encoding'] == 'utf-8' and not artifact['truncated']
            assert hashlib.sha256(artifact['content'].encode()).hexdigest() == ref['sha256']
        return {'status': value['status'], 'attempt': value['attempt'], 'unknownOutcome': 'execution_outcome_unknown' in value.get('error', ''), 'cleanupPending': bool(value.get('runtimeCleanupPending')), 'effects': facts['effects'], 'runningEvents': 1}

    try:
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0)); port = listener.getsockname()[1]
        forward = subprocess.Popen(KUBE + ['-n', 'rank-dev', 'port-forward', '--address=127.0.0.1', 'service/execution-api', str(port) + ':8790'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        base = 'http://127.0.0.1:' + str(port)
        wait(lambda: healthy(base), 20)
        capacity = request('/v1/capacity')
        if any(capacity[q][key] for q in ['online', 'evaluation'] for key in ['active', 'ready', 'resourceBlocked', 'quotaBlocked', 'capabilityBlocked', 'deliveryBlocked', 'expired', 'cleanupPending']):
            raise RuntimeError('Use an idle lab')
        if any(name.startswith('sandbox-') for name in run(['docker', 'ps', '--format', '{{.Names}}'], capture=True).splitlines()):
            raise RuntimeError('Sandbox host has active tasks')
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0)); model_port = listener.getsockname()[1]
        run(['docker', 'run', '-d', '--name', container, '--label', 'rank.lab=' + prefix, '--network', 'rank-dev', '-p', '127.0.0.1:' + str(model_port) + ':8800', '--cpus=0.5', '--memory=128m', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--user=1000:1000', '--mount', 'type=bind,src=' + str(HERE / 'fixtures/fault-model.mjs') + ',dst=/opt/fault-model.mjs,readonly', '--entrypoint', 'node', images['images']['platform'], '/opt/fault-model.mjs'], capture=True)
        created_model = True
        model = 'http://127.0.0.1:' + str(model_port)
        wait(lambda: healthy(model), 15)
        model_ip = run(['docker', 'inspect', container, '--format', '{{(index .NetworkSettings.Networks "rank-dev").IPAddress}}'], capture=True).strip()
        connection = request('/v1/model-connections', {'name': prefix, 'provider': 'custom', 'protocol': 'openai-chat-completions', 'baseUrl': 'http://' + model_ip + ':8800/v1', 'apiKey': 'rank-lab-fixture', 'defaultModel': 'rank-lab'})
        request('/v1/model-connections/' + connection['id'] + '/verify', {})
        watch = PodWatch()
        wait(lambda: all(watch.get(pod['metadata']['uid']) for pod in worker_pods()), 10)
        for label in (['complete-in-grace', 'interrupt-at-grace'] if automatic else ['complete-in-grace', 'interrupt-at-grace', 'rolling-update']):
            print('Active drain phase:', label, flush=True)
            pods = worker_pods()
            assert len(pods) == 2
            items = [submit('tool-' + label + '-' + str(number)) for number in range(2)]
            for item in items:
                value = wait(lambda item=item: held(item), 20)
                item['owner'] = owner(value, pods)
            assert items[0]['owner']['uid'] != items[1]['owner']['uid']
            scenario = {'name': label, 'executions': [item['id'] for item in items], 'workers': [], 'observed': []}
            report['scenarios'].append(scenario)
            mutated_workers = True
            if automatic:
                victim, survivor = items
                # Make Kubernetes choose the active owner rather than the idle one.
                for item, cost in [(victim, '-1000'), (survivor, '1000')]:
                    kubectl('annotate', 'pod/' + item['owner']['name'], 'controller.kubernetes.io/pod-deletion-cost=' + cost, '--overwrite')
                scaler_enabled = True; scaling.enable()
                kubectl('patch', 'scaledobject/execution-worker', '--type=merge', '-p', json.dumps({'spec': {'advanced': {'horizontalPodAutoscalerConfig': {'behavior': {'scaleDown': {'stabilizationWindowSeconds': 0}}}}}}))
                release(survivor)
                survivor_result = check_item(survivor, 'completed')
                wait(lambda: watch.get(victim['owner']['uid']).get('deletingAt'), 40)
                assert get(victim['id'])['status'] == 'running', 'Autoscaler must select a live task owner'
                hpa = json.loads(kubectl('get', 'hpa/keda-hpa-execution-worker', '-o', 'json'))
                assert hpa['status']['desiredReplicas'] == 1
                scenario['automaticDecision'] = {'hpaDesired': 1, 'activeOwnerSelected': True, 'stabilizationSeconds': 0, 'deletionCostSelectedVictim': True}
                if label == 'complete-in-grace': release(victim)
                fact = wait(lambda: exit_fact(victim), 35)
                assert fact['exitCode'] == 0
                elapsed = fact['exitObservedAt'] - fact['deletionObservedAt']
                assert elapsed < 25
                if label == 'interrupt-at-grace': assert elapsed >= 14
                scenario['workers'].append(fact)
                scenario['observed'] = [check_item(victim, 'completed' if label == 'complete-in-grace' else 'failed'), survivor_result]
                sentinel = submit(label + '-sentinel')
                scenario['sentinel'] = check_item(sentinel, 'completed')
                assert not get(sentinel['id'])['externalHandle'].startswith(victim['owner']['name'] + '-')
                scaling.disable(); scaler_enabled = False
                kubectl('annotate', 'pod/' + survivor['owner']['name'], 'controller.kubernetes.io/pod-deletion-cost-')
                wait(lambda: len(worker_pods()) == 2, 30)
            elif label != 'rolling-update':
                kubectl('scale', 'deployment/execution-worker', '--replicas=1')
                victim = wait(lambda: next((item for item in items if watch.get(item['owner']['uid']).get('deletingAt')), None), 15)
                survivor = next(item for item in items if item is not victim)
                queued = submit(label + '-sentinel')
                if label == 'complete-in-grace':
                    release(victim)
                deadline = time.monotonic() + 35
                while not exit_fact(victim):
                    assert get(queued['id'])['status'] == 'queued' and get(queued['id'])['attempt'] == 0
                    assert get(survivor['id'])['status'] == 'running'
                    if time.monotonic() > deadline:
                        raise TimeoutError('No terminal Worker container status was observed')
                    time.sleep(0.1)
                fact = exit_fact(victim)
                assert fact['exitCode'] == 0
                elapsed = fact['exitObservedAt'] - fact['deletionObservedAt']
                assert elapsed < 25
                if label == 'interrupt-at-grace':
                    assert elapsed >= 14
                scenario['workers'].append(fact)
                scenario['sentinelQueuedUntilExit'] = True
                expected = 'completed' if label == 'complete-in-grace' else 'failed'
                scenario['observed'].append(check_item(victim, expected))
                release(survivor)
                scenario['observed'].append(check_item(survivor, 'completed'))
                scenario['sentinel'] = check_item(queued, 'completed')
                assert get(queued['id'])['externalHandle'] == get(survivor['id'])['externalHandle']
                kubectl('scale', 'deployment/execution-worker', '--replicas=2')
                kubectl('rollout', 'status', 'deployment/execution-worker', '--timeout=60s')
                wait(lambda: len(worker_pods()) == 2, 30)
            else:
                kubectl('rollout', 'restart', 'deployment/execution-worker')
                released = set()
                queued = None
                deadline = time.monotonic() + 60
                while len(released) < len(items):
                    for item in items:
                        if item['id'] not in released and watch.get(item['owner']['uid']).get('deletingAt'):
                            if queued is None:
                                queued = submit(label + '-sentinel')
                            release(item)
                            released.add(item['id'])
                    if time.monotonic() > deadline:
                        raise TimeoutError('Rolling update did not replace both active owners')
                    time.sleep(0.1)
                for item in items:
                    fact = wait(lambda item=item: exit_fact(item), 30)
                    assert fact['exitCode'] == 0
                    scenario['workers'].append(fact)
                    scenario['observed'].append(check_item(item, 'completed'))
                scenario['sentinel'] = check_item(queued, 'completed')
                assert not any(get(queued['id'])['externalHandle'].startswith(item['owner']['name'] + '-') for item in items)
                kubectl('rollout', 'status', 'deployment/execution-worker', '--timeout=60s')
                wait(lambda: len(worker_pods()) == 2, 30)
            scenario['exitCodes'] = [fact['exitCode'] for fact in scenario['workers']]
        observed = [{key: scenario[key] for key in ['name', 'observed', 'sentinel', 'exitCodes']} for scenario in report['scenarios']]
        expected = json.loads((HERE / 'fixtures/active-drain.snapshot.json').read_text())
        assert observed == (expected[:2] if automatic else expected), json.dumps(observed)
        report['automatic'] = automatic
        report['passed'] = True
    except Exception as error:
        report['failure'] = type(error).__name__
        raise
    finally:
        cleanup_errors = []
        def cleanup(name, operation):
            try:
                operation()
            except Exception as error:
                cleanup_errors.append({'operation': name, 'error': type(error).__name__})
        def stop_work():
            for identifier in report['submittedExecutionIds']:
                value = get(identifier)
                if value['status'] in ['queued', 'running', 'cancelling']:
                    request('/v1/executions/' + identifier + '/cancel', {})
            wait(lambda: all(settled(identifier) for identifier in report['submittedExecutionIds']), 100)
        if scaler_enabled: cleanup('disable experiment scaler', scaling.disable)
        cleanup('settle owned executions', stop_work)
        if mutated_workers:
            def restore():
                kubectl('scale', 'deployment/execution-worker', '--replicas=2')
                kubectl('patch', 'deployment/execution-worker', '--type=json', '-p', json.dumps([{'op': 'add', 'path': '/spec/template/metadata/annotations', 'value': original_annotations}]))
                kubectl('rollout', 'status', 'deployment/execution-worker', '--timeout=60s')
                wait(lambda: len(worker_pods()) == 2, 30)
            cleanup('restore two Workers and rollout annotations', restore)
        if watch is not None:
            cleanup('stop Pod watch', watch.close)
        if forward is not None:
            forward.terminate()
            cleanup('stop port forward', lambda: forward.wait(timeout=10))
        if created_model:
            cleanup('remove model fixture', lambda: run(['docker', 'rm', '-f', container], capture=True))
        report['cleanupErrors'] = cleanup_errors
        if cleanup_errors:
            report['passed'] = False
        report['endedAt'] = datetime.now(timezone.utc).isoformat()
        path = STATE / (prefix + ('-keda-drain.json' if automatic else '-active-drain.json'))
        path.write_text(json.dumps(report, indent=2) + '\n')
        print('Active drain evidence:', path, flush=True)
        if cleanup_errors:
            raise RuntimeError('Active drain cleanup failed; inspect the retained report')


if __name__ == '__main__':
    main()
