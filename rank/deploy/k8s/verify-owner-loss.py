"""Kill confirmed Worker containers after real DSH tool effects; never replay unknown work."""
from datetime import datetime, timezone
import json
import socket
import subprocess
import time
import urllib.error
import urllib.request
import uuid

from lab import KUBE, HERE, STATE, run
from worker_fault import kill_owner
from verify import kubectl, wait


def verify_snapshot(report):
    """Compare observed API outcomes and tool effects with the keyless lab transcript."""
    observed = []
    for scenario in report['scenarios']:
        outcome = scenario['outcome']
        facts = scenario['facts']
        observed.append({
            'name': scenario['name'],
            'status': outcome['status'],
            'attempt': outcome['attempt'],
            'unknownOutcome': 'execution_outcome_unknown' in (outcome['error'] or ''),
            'cleanupPending': bool(outcome['runtimeCleanupPending']),
            'ownerExitCode': scenario['ownerLoss']['exitCode'],
            'runningEvents': scenario['lifecycle'].count('execution.running'),
            'effects': facts['effects'],
            'closedEffects': facts['closedEffects'],
            'deniedWrites': facts['deniedWrites'],
        })
    expected = json.loads((HERE / 'fixtures/owner-loss.snapshot.json').read_text())
    if observed != expected:
        raise AssertionError('Owner-loss transcript differs: ' + json.dumps(observed))
    return observed


def main():
    mode = json.loads((STATE / 'platform-mode.json').read_text())
    if mode['executor'] != 'opensandbox':
        raise RuntimeError('Owner-loss verification requires the remote platform lab')
    if kubectl('get', 'scaledobject/execution-worker', '--ignore-not-found', '-o', 'name').strip():
        raise RuntimeError('Disable lab autoscaling before fault injection')
    token = json.loads((STATE / 'execution-lab.json').read_text())['EXECUTION_SERVICE_TOKEN']
    images = json.loads((STATE / 'platform-images.json').read_text())
    prefix = 'fault-' + uuid.uuid4().hex
    container = 'rank-' + prefix
    report = {'startedAt': datetime.now(timezone.utc).isoformat(), 'images': images, 'scenarios': [], 'submittedExecutionIds': []}
    forward = None
    created_fixture = False
    base = ''

    def request(path, body=None, raw=False):
        req = urllib.request.Request(base + path, headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'}, data=None if body is None else json.dumps(body).encode())
        with urllib.request.urlopen(req, timeout=30) as response:
            return response.read().decode() if raw else json.load(response)

    def get(identifier):
        return request('/v1/executions/' + identifier)


    try:
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0)); api_port = listener.getsockname()[1]
        forward = subprocess.Popen(KUBE + ['-n', 'rank-dev', 'port-forward', '--address=127.0.0.1', 'service/execution-api', str(api_port) + ':8790'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        base = 'http://127.0.0.1:' + str(api_port)
        def healthy():
            try:
                with urllib.request.urlopen(base + '/v1/health', timeout=2) as response:
                    return response.status == 200
            except (urllib.error.URLError, OSError):
                return False
        wait(healthy, 30)
        capacity = request('/v1/capacity')
        if any(capacity[q][key] for q in ['online', 'evaluation'] for key in ['active', 'ready', 'resourceBlocked', 'quotaBlocked', 'capabilityBlocked', 'deliveryBlocked', 'expired', 'cleanupPending']):
            raise RuntimeError('Use an idle lab for owner-loss injection')
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0)); model_port = listener.getsockname()[1]
        run(['docker', 'run', '-d', '--name', container, '--label', 'rank.lab=' + prefix, '--network', 'rank-dev', '-p', '127.0.0.1:' + str(model_port) + ':8800', '--cpus=0.5', '--memory=128m', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--user=1000:1000', '--mount', 'type=bind,src=' + str(HERE / 'fixtures/fault-model.mjs') + ',dst=/opt/fault-model.mjs,readonly', '--entrypoint', 'node', images['images']['platform'], '/opt/fault-model.mjs'], capture=True)
        created_fixture = True
        model_ip = run(['docker', 'inspect', container, '--format', '{{(index .NetworkSettings.Networks "rank-dev").IPAddress}}'], capture=True).strip()
        def facts():
            with urllib.request.urlopen('http://127.0.0.1:' + str(model_port) + '/facts', timeout=3) as response:
                return json.load(response)
        def model_ready():
            try:
                with urllib.request.urlopen('http://127.0.0.1:' + str(model_port) + '/health', timeout=2) as response:
                    return response.status == 200
            except (urllib.error.URLError, OSError):
                return False
        wait(model_ready, 15)
        connection = request('/v1/model-connections', {'name': prefix, 'provider': 'custom', 'protocol': 'openai-chat-completions', 'baseUrl': 'http://' + model_ip + ':8800/v1', 'apiKey': 'rank-lab-fixture', 'defaultModel': 'rank-lab'})
        request('/v1/model-connections/' + connection['id'] + '/verify', {})
        report['modelFixture'] = {'image': images['images']['platform'], 'delaySeconds': 90}
        for label in ['tool-loss', 'tool-cancel', 'completed']:
            key = prefix + '-' + label
            scenario = {'name': label}
            report['scenarios'].append(scenario)
            body = {'idempotencyKey': key, 'spec': {'protocolVersion': 2, 'kind': 'agent', 'harness': 'dsh', 'prompt': key, 'modelConnectionId': connection['id'], 'model': 'rank-lab', 'timeoutMs': 20000, 'metadata': {'responseId': key, 'tenantId': prefix}}}
            value = request('/v1/executions', body)
            identifier = value['id']; report['submittedExecutionIds'].append(identifier)
            scenario['executionId'] = identifier
            print('Fault phase:', label, 'waiting for observed work', flush=True)
            if label == 'completed':
                value = wait(lambda: (current if (current := get(identifier))['status'] in ['completed', 'failed'] else None), 40)
                assert value['status'] == 'completed'
                scenario['ownerLoss'] = kill_owner(value)
            else:
                def effect_seen():
                    current = get(identifier)
                    if current['status'] in ['completed', 'failed', 'cancelled']:
                        scenario['error'] = current.get('error'); raise AssertionError('Harness failed before the tool effect')
                    return current if facts().get(key, {}).get('effects') == 1 else None
                value = wait(effect_seen, 20)
                assert value['runtime']['handle'] and value['runtimeCleanupPending']
                scenario['observedEffectBeforeCrash'] = facts()[key]
                scenario['runtimeHandle'] = value['runtime']['handle']
                scenario['ownerLoss'] = kill_owner(value)
                if label == 'tool-cancel':
                    request('/v1/executions/' + identifier + '/cancel', {})
                last_status = None
                deadline = time.monotonic() + 160
                while True:
                    current = get(identifier)
                    state = (current['status'], bool(current.get('runtimeCleanupPending')))
                    if state != last_status:
                        print('Fault phase:', label, state, flush=True); last_status = state
                    if current['status'] in ['completed', 'failed', 'cancelled'] and not current.get('runtimeCleanupPending'):
                        value = current; break
                    if time.monotonic() > deadline:
                        scenario['lastOutcome'] = {k:current.get(k) for k in ['status', 'error', 'attempt', 'runtimeCleanupPending']}
                        raise TimeoutError('Owner-loss recovery did not converge')
                    time.sleep(0.5)
                assert value['status'] == 'failed' and 'execution_outcome_unknown' in value['error'] and value['attempt'] == 1
                assert facts()[key]['effects'] == 1 and facts()[key]['deniedWrites'] == 1
                inspected = subprocess.run(['docker', 'inspect', 'sandbox-' + scenario['runtimeHandle'], '--format', '{{.State.Running}}'], capture_output=True, text=True)
                assert inspected.returncode != 0 and 'No such object' in inspected.stderr
            replay = request('/v1/executions', body)
            assert replay['id'] == identifier and replay['status'] == value['status'] and replay['attempt'] == 1
            events = [json.loads(line[6:]) for line in request('/v1/executions/' + identifier + '/events', raw=True).splitlines() if line.startswith('data: ')]
            assert sum(event['type'] == 'execution.running' for event in events) == 1
            scenario.update(outcome={k:value.get(k) for k in ['status', 'attempt', 'error', 'runtimeCleanupPending']}, facts=facts()[key], lifecycle=[event['type'] for event in events if event['type'].startswith('execution.')])
        report['snapshot'] = verify_snapshot(report)
        report['passed'] = True
    except Exception as error:
        report['failure'] = type(error).__name__
        raise
    finally:
        if forward is not None:
            forward.terminate(); forward.wait(timeout=10)
        if created_fixture:
            try:
                report['finalModelFacts'] = facts()
            except Exception as error:
                report['factsReadFailure'] = type(error).__name__
            run(['docker', 'rm', '-f', container], capture=True)
        report['endedAt'] = datetime.now(timezone.utc).isoformat()
        path = STATE / (prefix + '.json')
        path.write_text(json.dumps(report, indent=2) + '\n')
        print('Owner-loss evidence:', path, flush=True)


if __name__ == '__main__':
    main()
