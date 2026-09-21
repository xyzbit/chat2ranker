"""Verify frozen queue deadlines across zero workers and Execution API replacement."""
from datetime import datetime, timezone
import json
import socket
import subprocess
import urllib.error
import urllib.request
import uuid

from lab import KUBE, STATE
from verify import kubectl, scale, wait


def main():
    if kubectl('get', 'scaledobject/execution-worker', '--ignore-not-found', '-o', 'name').strip():
        raise RuntimeError('Disable the lab scaler before verifying queue deadlines')
    token = json.loads((STATE / 'execution-lab.json').read_text())['EXECUTION_SERVICE_TOKEN']
    mode = json.loads((STATE / 'platform-mode.json').read_text())
    prefix = 'queue-deadline-' + uuid.uuid4().hex
    report = {'startedAt': datetime.now(timezone.utc).isoformat(), 'checks': {}, 'images': json.loads((STATE / 'platform-images.json').read_text())}
    forward = None
    base = ''
    changed = False

    def connect():
        nonlocal forward, base
        if forward is not None:
            forward.terminate()
            forward.wait(timeout=10)
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            port = listener.getsockname()[1]
        forward = subprocess.Popen(KUBE + ['-n', 'rank-dev', 'port-forward', '--address=127.0.0.1', 'service/execution-api', str(port) + ':8790'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        base = 'http://127.0.0.1:' + str(port)
        def healthy():
            try:
                with urllib.request.urlopen(base + '/v1/health', timeout=2) as response:
                    return response.status == 200
            except (urllib.error.URLError, OSError):
                return False
        wait(healthy, 30)

    def request(path, body=None, raw=False):
        req = urllib.request.Request(base + path, headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'}, data=None if body is None else json.dumps(body).encode())
        with urllib.request.urlopen(req, timeout=30) as response:
            return response.read().decode() if raw else json.load(response)

    def terminal(identifier):
        value = request('/v1/executions/' + identifier)
        return value if value['status'] in ['completed', 'failed', 'cancelled'] and not value.get('runtimeCleanupPending') else None

    try:
        connect()
        capacity = request('/v1/capacity')
        if any(capacity[q][key] for q in ['online', 'evaluation'] for key in ['active', 'ready', 'resourceBlocked', 'quotaBlocked', 'capabilityBlocked', 'deliveryBlocked', 'expired', 'cleanupPending']):
            raise RuntimeError('Run queue verification on an idle lab')
        connection = request('/v1/model-connections', {'name': prefix, 'provider': 'custom', 'protocol': 'openai-chat-completions', 'baseUrl': mode['modelURL'], 'apiKey': 'rank-lab-fixture', 'defaultModel': 'rank-lab'})
        request('/v1/model-connections/' + connection['id'] + '/verify', {})
        changed = True
        scale(0)
        body = {'idempotencyKey': prefix, 'spec': {'protocolVersion': 2, 'kind': 'agent', 'harness': 'dsh', 'prompt': 'Remember Ada', 'modelConnectionId': connection['id'], 'model': 'rank-lab', 'queueTimeoutMs': 1000, 'metadata': {'responseId': prefix, 'tenantId': prefix}}}
        queued = request('/v1/executions', body)
        assert queued['status'] == 'queued' and queued['attempt'] == 0
        report['executionId'] = queued['id']
        report['queueExpiresAt'] = queued['queueExpiresAt']
        kubectl('rollout', 'restart', 'deployment/execution-api')
        kubectl('rollout', 'status', 'deployment/execution-api', '--timeout=60s')
        connect()
        expired = wait(lambda: terminal(queued['id']), 30)
        assert expired['status'] == 'failed' and expired['attempt'] == 0 and not expired.get('runtime') and not expired.get('startedAt')
        assert expired['queueExpiresAt'] == queued['queueExpiresAt']
        assert expired['error'].startswith('execution_queue_expired:')
        report['checks']['expiresWithZeroWorkersAfterAPIReplacement'] = True
        replay = request('/v1/executions', body)
        assert replay['id'] == expired['id'] and replay['status'] == 'failed'
        stream = request('/v1/executions/' + expired['id'] + '/events', raw=True)
        events = [json.loads(line[6:]) for line in stream.splitlines() if line.startswith('data: ')]
        assert [event['type'] for event in events] == ['execution.queued', 'execution.failed']
        report['checks']['durableFailureReplay'] = True
        scale(2)
        body['idempotencyKey'] += '-fresh'
        body['spec']['queueTimeoutMs'] = 60000
        body['spec']['metadata']['responseId'] += '-fresh'
        fresh = request('/v1/executions', body)
        completed = wait(lambda: terminal(fresh['id']), 60)
        assert completed['status'] == 'completed' and completed['attempt'] == 1
        if mode['executor'] == 'opensandbox':
            assert completed.get('runtime', {}).get('handle')
        late = request('/v1/executions/' + expired['id'])
        assert late['status'] == 'failed' and late['attempt'] == 0 and not late.get('runtimeCleanupPending')
        report['checks']['restoredWorkersRunOnlyFreshInvocation'] = True
        report['outcomes'] = [{'id': value['id'], 'status': value['status'], 'attempt': value['attempt'], 'cleanupPending': bool(value.get('runtimeCleanupPending'))} for value in [late, completed]]
    except Exception as error:
        report['failure'] = type(error).__name__
        raise
    finally:
        try:
            if changed:
                scale(2)
        finally:
            if forward is not None:
                forward.terminate()
                forward.wait(timeout=10)
            report['endedAt'] = datetime.now(timezone.utc).isoformat()
            path = STATE / (prefix + '.json')
            path.write_text(json.dumps(report, indent=2) + '\n')
            print('Queue deadline evidence:', path)


if __name__ == '__main__':
    main()
