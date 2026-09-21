"""Interrupt PostgreSQL and real S3 upload streams in an idle remote platform lab."""
from datetime import datetime, timezone
import base64
import hashlib
import json
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

from lab import HERE, KUBE, STATE, apply_json, run
from verify import kubectl, wait


def main():
    if json.loads((STATE / 'platform-mode.json').read_text())['executor'] != 'opensandbox':
        raise RuntimeError('Install the remote platform first')
    if kubectl('get', 'scaledobject/execution-worker', '--ignore-not-found', '-o', 'name').strip():
        raise RuntimeError('Disable autoscaling before dependency faults')
    token = json.loads((STATE / 'execution-lab.json').read_text())['EXECUTION_SERVICE_TOKEN']
    images = json.loads((STATE / 'platform-images.json').read_text())
    prefix = 'fault-' + uuid.uuid4().hex
    fixture = 'rank-' + prefix
    report = {'startedAt': datetime.now(timezone.utc).isoformat(), 'images': images, 'scenarios': []}
    forwards, identifiers = [], []
    model_created = proxy_created = route_changed = postgres_stopped = False
    original_selector = None

    def forward(target, remote_port):
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0)); port = listener.getsockname()[1]
        process = subprocess.Popen(KUBE + ['-n', 'rank-dev', 'port-forward', '--address=127.0.0.1', target, str(port) + ':' + str(remote_port)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        forwards.append(process)
        return 'http://127.0.0.1:' + str(port)

    def http(base, path, body=None, authenticated=False, raw=False, timeout=5):
        headers = {'Content-Type': 'application/json'}
        if authenticated: headers['Authorization'] = 'Bearer ' + token
        req = urllib.request.Request(base + path, headers=headers, data=None if body is None else json.dumps(body).encode())
        with urllib.request.urlopen(req, timeout=timeout) as response:
            return response.read().decode() if raw else json.load(response)

    def reachable(base, path):
        try: return http(base, path) is not None
        except (urllib.error.URLError, OSError): return False

    def request(path, body=None, raw=False):
        return http(api, path, body, authenticated=True, raw=raw, timeout=10)

    def settled(identifier):
        value = request('/v1/executions/' + identifier)
        return value if value['status'] in ['completed', 'failed', 'cancelled'] and not value.get('runtimeCleanupPending') else None

    def artifact_content(value):
        ref = next(ref for ref in value['result']['artifacts'] if ref['kind'] == 'stdout')
        content = request('/v1/executions/' + value['id'] + '/artifacts?path=' + urllib.parse.quote(ref['path']))
        assert not content['truncated']
        assert hashlib.sha256(base64.b64decode(content['content']) if content['encoding'] == 'base64' else content['content'].encode()).hexdigest() == ref['sha256']
        return ref['sha256']

    try:
        api = forward('service/execution-api', 8790)
        wait(lambda: reachable(api, '/v1/health'), 20)
        capacity = request('/v1/capacity')
        if any(capacity[q][key] for q in ['online', 'evaluation'] for key in ['active', 'ready', 'resourceBlocked', 'quotaBlocked', 'capabilityBlocked', 'deliveryBlocked', 'expired', 'cleanupPending']):
            raise RuntimeError('Use an idle lab')
        if any(name.startswith('sandbox-') for name in run(['docker', 'ps', '--format', '{{.Names}}'], capture=True).splitlines()):
            raise RuntimeError('Use an idle Sandbox host')
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0)); model_port = listener.getsockname()[1]
        run(['docker', 'run', '-d', '--name', fixture, '--network', 'rank-dev', '-p', '127.0.0.1:' + str(model_port) + ':8800', '--cpus=0.5', '--memory=128m', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--user=1000:1000', '--mount', 'type=bind,src=' + str(HERE / 'fixtures/fault-model.mjs') + ',dst=/opt/fault-model.mjs,readonly', '--entrypoint', 'node', images['images']['platform'], '/opt/fault-model.mjs'], capture=True)
        model_created = True
        model = 'http://127.0.0.1:' + str(model_port)
        wait(lambda: reachable(model, '/health'), 15)
        model_ip = run(['docker', 'inspect', fixture, '--format', '{{(index .NetworkSettings.Networks "rank-dev").IPAddress}}'], capture=True).strip()
        connection = request('/v1/model-connections', {'name': prefix, 'provider': 'custom', 'protocol': 'openai-chat-completions', 'baseUrl': 'http://' + model_ip + ':8800/v1', 'apiKey': 'rank-lab-fixture', 'defaultModel': 'rank-lab'})
        request('/v1/model-connections/' + connection['id'] + '/verify', {})

        def submit(key):
            body = {'idempotencyKey': key, 'spec': {'protocolVersion': 2, 'kind': 'agent', 'harness': 'dsh', 'prompt': key, 'modelConnectionId': connection['id'], 'model': 'rank-lab', 'timeoutMs': 20000, 'metadata': {'responseId': key, 'tenantId': prefix}}}
            value = request('/v1/executions', body)
            identifiers.append(value['id'])
            return value, body

        objects = json.loads(kubectl('get', 'pods', '-l', 'app=objects', '-o', 'json'))['items']
        assert len(objects) == 1
        upstream = objects[0]['status']['podIP']
        apply_json({'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': fixture, 'namespace': 'rank-dev'}, 'data': {'fault-s3.mjs': (HERE / 'fixtures/fault-s3.mjs').read_text()}})
        proxy_created = True
        apply_json({'apiVersion': 'v1', 'kind': 'Pod', 'metadata': {'name': fixture, 'namespace': 'rank-dev', 'labels': {'app': fixture}}, 'spec': {'automountServiceAccountToken': False, 'terminationGracePeriodSeconds': 2, 'securityContext': {'runAsNonRoot': True, 'runAsUser': 1000}, 'containers': [{'name': 'proxy', 'image': images['images']['platform'], 'imagePullPolicy': 'Never', 'command': ['node', '/fixture/fault-s3.mjs'], 'env': [{'name': 'UPSTREAM_IP', 'value': upstream}], 'securityContext': {'readOnlyRootFilesystem': True, 'allowPrivilegeEscalation': False, 'capabilities': {'drop': ['ALL']}}, 'resources': {'requests': {'cpu': '50m', 'memory': '32Mi'}, 'limits': {'cpu': '500m', 'memory': '128Mi'}}, 'volumeMounts': [{'name': 'fixture', 'mountPath': '/fixture', 'readOnly': True}], 'readinessProbe': {'httpGet': {'path': '/facts', 'port': 8334}, 'periodSeconds': 1}}], 'volumes': [{'name': 'fixture', 'configMap': {'name': fixture}}]}})
        kubectl('wait', '--for=condition=Ready', 'pod/' + fixture, '--timeout=30s')
        proxy = forward('pod/' + fixture, 8334)
        wait(lambda: reachable(proxy, '/facts'), 15)
        original_selector = json.loads(kubectl('get', 'service/objects', '-o', 'json'))['spec']['selector']
        route_changed = True
        kubectl('patch', 'service/objects', '--type=json', '-p', json.dumps([{'op': 'replace', 'path': '/spec/selector', 'value': {'app': fixture}}]))
        proxy_ip = json.loads(kubectl('get', 'pod/' + fixture, '-o', 'json'))['status']['podIP']
        wait(lambda: proxy_ip in kubectl('get', 'endpointslices', '-l', 'kubernetes.io/service-name=objects', '-o', 'json'), 15)
        kubectl('rollout', 'restart', 'deployment/execution-worker')
        kubectl('rollout', 'status', 'deployment/execution-worker', '--timeout=60s')

        baseline, _ = submit(prefix + '-baseline')
        baseline = wait(lambda: settled(baseline['id']), 35)
        assert baseline['status'] == 'completed'
        baseline_digest = artifact_content(baseline)

        for label in ['database', 'upload']:
            print('Dependency phase:', label, flush=True)
            key = prefix + '-tool-' + label
            value, body = submit(key)
            identifier = value['id']
            wait(lambda: http(model, '/facts').get(key, {}).get('effects') == 1, 20)
            value = request('/v1/executions/' + identifier)
            assert value.get('runtime', {}).get('handle') and value['attempt'] == 1
            runtime_handle = value['runtime']['handle']
            if label == 'database':
                postgres_stopped = True
                kubectl('scale', 'deployment/postgres', '--replicas=0')
                kubectl('wait', '--for=delete', 'pod', '-l', 'app=postgres', '--timeout=45s')
                outage_start = time.monotonic()
                try:
                    request('/v1/executions/' + identifier)
                    raise AssertionError('Database outage returned a normal execution read')
                except urllib.error.HTTPError as error:
                    assert error.code >= 500
                    report['outageRead'] = 'http_' + str(error.code)
                report['outageReadSeconds'] = round(time.monotonic() - outage_start, 3)
                assert report['outageReadSeconds'] < 8, 'Database outage did not return a bounded server error'
                # Exceed the three-second persistence window with the database actually absent.
                deadline = time.monotonic() + 7
                while time.monotonic() < deadline: time.sleep(0.25)
                kubectl('scale', 'deployment/postgres', '--replicas=1')
                kubectl('rollout', 'status', 'deployment/postgres', '--timeout=60s')
                postgres_stopped = False
            else:
                http(proxy, '/arm', {'executionID': identifier})
                http(model, '/release?key=' + key, {})
            current = wait(lambda: settled(identifier), 160)
            if label == 'upload':
                proxy_facts = http(proxy, '/facts')
                assert proxy_facts['interrupted'] >= 1 and proxy_facts['receivedBytes'] > proxy_facts['forwardedBytes'] > 0
                assert len(proxy_facts['completedKeys']) == 1
                kubectl('patch', 'service/objects', '--type=json', '-p', json.dumps([{'op': 'replace', 'path': '/spec/selector', 'value': original_selector}]))
                route_changed = False
                wait(lambda: upstream in kubectl('get', 'endpointslices', '-l', 'kubernetes.io/service-name=objects', '-o', 'json'), 15)
                assert current['result']['output'] and current['result']['usage']['inputTokens'] > 0
                assert len(current['result']['artifacts']) == 1
                for ref in current['result']['artifacts']:
                    content = request('/v1/executions/' + identifier + '/artifacts?path=' + urllib.parse.quote(ref['path']))
                    assert not content['truncated'] and hashlib.sha256(base64.b64decode(content['content']) if content['encoding'] == 'base64' else content['content'].encode()).hexdigest() == ref['sha256']
                report['uploadProxy'] = proxy_facts
            assert current['status'] == 'failed' and current['attempt'] == 1
            replay = request('/v1/executions', body)
            assert replay['id'] == identifier and replay['status'] == 'failed' and replay['attempt'] == 1
            events = [json.loads(line[6:]) for line in request('/v1/executions/' + identifier + '/events', raw=True).splitlines() if line.startswith('data: ')]
            facts = http(model, '/facts')[key]
            inspected = subprocess.run(['docker', 'inspect', 'sandbox-' + runtime_handle, '--format', '{{.State.Running}}'], capture_output=True, text=True)
            assert inspected.returncode and 'No such object' in inspected.stderr
            observation = {'name': label, 'status': current['status'], 'attempt': current['attempt'], 'unknownOutcome': 'execution_outcome_unknown' in current.get('error', ''), 'cleanupPending': bool(current.get('runtimeCleanupPending')), 'effects': facts['effects'], 'runningEvents': sum(event['type'] == 'execution.running' for event in events), 'evidenceRetained': bool(current.get('result'))}
            if label == 'database':
                observation['boundedDependencyError'] = report['outageRead'].startswith('http_5') and report['outageReadSeconds'] < 8
            report['scenarios'].append(observation)
            assert artifact_content(baseline) == baseline_digest
        assert report['scenarios'] == json.loads((HERE / 'fixtures/dependencies.snapshot.json').read_text())
        fresh, _ = submit(prefix + '-recovered')
        fresh = wait(lambda: settled(fresh['id']), 35)
        assert fresh['status'] == 'completed' and fresh['attempt'] == 1
        artifact_content(fresh)
        report['passed'] = True
    except Exception as error:
        report['failure'] = type(error).__name__
        raise
    finally:
        if postgres_stopped:
            kubectl('scale', 'deployment/postgres', '--replicas=1')
            kubectl('rollout', 'status', 'deployment/postgres', '--timeout=60s')
        if route_changed:
            kubectl('patch', 'service/objects', '--type=json', '-p', json.dumps([{'op': 'replace', 'path': '/spec/selector', 'value': original_selector}]))
        for identifier in identifiers:
            try:
                if not settled(identifier):
                    request('/v1/executions/' + identifier + '/cancel', {})
                    wait(lambda: settled(identifier), 160)
            except Exception as error:
                report.setdefault('cleanupFailures', []).append(type(error).__name__)
        for process in forwards:
            process.terminate(); process.wait(timeout=10)
        if proxy_created:
            try:
                kubectl('delete', 'pod/' + fixture, 'configmap/' + fixture, '--ignore-not-found', '--wait=true', '--timeout=60s')
            except Exception as error:
                report.setdefault('cleanupFailures', []).append(type(error).__name__)
        if model_created:
            try: run(['docker', 'rm', '-f', fixture], capture=True)
            except Exception as error: report.setdefault('cleanupFailures', []).append(type(error).__name__)
        report['endedAt'] = datetime.now(timezone.utc).isoformat()
        path = STATE / (prefix + '-dependencies.json')
        path.write_text(json.dumps(report, indent=2) + '\n')
        print('Dependency evidence:', path, flush=True)
        if report.get('cleanupFailures'): raise RuntimeError('Dependency experiment cleanup is incomplete; inspect the private report')


if __name__ == '__main__':
    main()
