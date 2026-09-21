"""Build and deploy the complete trusted Rank lab over the existing Execution dependencies."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess

from lab import HERE, ROOT, STATE, KUBE, apply_json, run
from database_roles import REVISION, database_url, provision, grant_runtime


def build(stage):
    runtime = stage / 'runtime'
    if not (stage / 'runtime.json').exists():
        raise RuntimeError('Run stage-runtime.py first')
    arch = json.loads((stage / 'runtime.json').read_text())['arch']
    goarch = 'amd64' if arch == 'x64' else arch
    for name, module in [('rankd', 'rank'), ('executiond', 'execution'), ('execution-worker', 'execution')]:
        run(['go', 'build', '-trimpath', '-o', str(stage / name), './cmd/' + name],
            cwd=ROOT / module / 'backend', env=dict(os.environ, GOTOOLCHAIN='go1.26.0', CGO_ENABLED='0', GOOS='linux', GOARCH=goarch))
    for name in ['platform.Dockerfile', 'gateway.Dockerfile', 'gateway.conf']:
        shutil.copy2(HERE / name, stage / name)
    for source, target in [(HERE / 'fixtures', stage / 'fixtures'), (ROOT / 'rank/assets', stage / 'rank-assets')]:
        shutil.copytree(source, target, dirs_exist_ok=True)
    digest = hashlib.sha256()
    for parent in [runtime, stage / 'fixtures', stage / 'rank-assets']:
        for path in sorted(parent.rglob('*')):
            if path.is_file():
                digest.update(str(path.relative_to(stage)).encode())
                digest.update(path.read_bytes())
    for name in ['rankd', 'executiond', 'execution-worker', 'platform.Dockerfile', 'gateway.Dockerfile', 'gateway.conf']:
        digest.update((stage / name).read_bytes())
    tag = digest.hexdigest()[:16]
    images = {'platform': 'rank-platform-lab:' + tag, 'gateway': 'rank-gateway-lab:' + tag}
    for name, filename in [('platform', 'platform.Dockerfile'), ('gateway', 'gateway.Dockerfile')]:
        run(['docker', 'build', '--provenance=false', '--platform=linux/' + goarch, '-f', str(stage / filename), '-t', images[name], str(stage)])
    record = {'images': images, 'stage': str(stage), 'arch': goarch,
              'digests': {name: run(['docker', 'image', 'inspect', image, '--format', '{{.Id}}'], capture=True).strip() for name, image in images.items()}}
    (STATE / 'platform-images.json').write_text(json.dumps(record, indent=2) + '\n')
    print('Built platform images:', images)


def secret(name, values):
    apply_json({'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': name, 'namespace': 'rank-dev'}, 'stringData': values})


def drain_platform():
    """Stop lab producers before consumers so schema upgrades have no old writers."""
    has_scaler_api = run(KUBE + ['get', 'crd/scaledobjects.keda.sh', '--ignore-not-found', '-o', 'name'], capture=True).strip()
    if has_scaler_api and run(KUBE + ['-n', 'rank-dev', 'get', 'scaledobject/execution-worker', '--ignore-not-found', '-o', 'name'], capture=True).strip():
        raise RuntimeError('Disable the lab autoscaler before the maintenance upgrade')
    existing = json.loads(run(KUBE + ['-n', 'rank-dev', 'get', 'deployments', '-o', 'json'], capture=True))
    deployments = {item['metadata']['name']: item for item in existing['items']}
    for name in ['rank-gateway', 'rank-api', 'rank-dispatcher', 'rank-control', 'execution-api', 'execution-worker']:
        if name not in deployments:
            continue
        selector = deployments[name]['spec']['selector']['matchLabels']
        labels = ','.join(key + '=' + value for key, value in selector.items())
        run(KUBE + ['-n', 'rank-dev', 'scale', 'deployment/' + name, '--replicas=0'])
        pods = json.loads(run(KUBE + ['-n', 'rank-dev', 'get', 'pods', '-l', labels, '-o', 'json'], capture=True))
        names = ['pod/' + pod['metadata']['name'] for pod in pods['items']]
        if names:
            run(KUBE + ['-n', 'rank-dev', 'wait', '--for=delete', *names, '--timeout=90s'])


def up(use_sandbox=False):
    record = json.loads((STATE / 'platform-images.json').read_text())
    images = record['images']
    sandbox_network = None
    if use_sandbox:
        sandbox_network = json.loads((STATE / 'sandbox-network.json').read_text())
        if sandbox_network['network'] != 'rank-dev':
            raise RuntimeError('Run sandbox-lab.py up --network rank-dev before installing the remote backend')
        sandbox_image = json.loads((STATE / 'sandbox-image.json').read_text())['image']
        sandbox_key = json.loads((STATE / 'opensandbox-lab.json').read_text())['apiKey']
    for image in images.values():
        run(['minikube', '-p', 'rank-dev', 'image', 'load', '--daemon', image], env=dict(os.environ, KUBECONFIG=str(STATE / 'config')))
    drain_platform()
    path = STATE / 'platform-lab.json'
    if not path.exists():
        data = {key: secrets.token_hex(32) for key in ['RANK_ACTION_SECRET', 'RANK_CONTROL_TOKEN', 'GATEWAY_PASSWORD']}
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as file:
            json.dump(data, file)
    data = json.loads(path.read_text())
    existing = json.loads((STATE / 'execution-lab.json').read_text())
    provision()
    runtime = {key: value for key, value in existing.items() if key != 'POSTGRES_PASSWORD'}
    quota_path = STATE / 'model-quota-key'
    if not quota_path.exists():
        with os.fdopen(os.open(quota_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as file:
            file.write(secrets.token_hex(32))
    runtime['EXECUTION_MODEL_QUOTA_KEY'] = quota_path.read_text().strip()
    runtime['EXECUTION_MODEL_QUOTA_URL'] = 'http://execution-api:8790/v1/model-permits'
    runtime.update({'EXECUTION_DATABASE_URL': database_url('execution_runtime'), 'EXECUTION_WORKER_VERSION': images['platform'].split(':')[1], 'RANK_CONTROL_TOKEN': data['RANK_CONTROL_TOKEN']})
    if use_sandbox:
        policy = json.loads((STATE / 'sandbox-egress-policy.json').read_text())
        node_ip = run(['minikube', '-p', 'rank-dev', 'ip'], capture=True).strip()
        callback_rule = {'protocol': 'tcp', 'cidr': node_ip + '/32', 'port': 30091}
        if callback_rule not in policy['allow']:
            policy['allow'].append(callback_rule)
        (STATE / 'sandbox-egress-policy.json').write_text(json.dumps(policy, indent=2) + '\n')
        runtime['EXECUTION_MODEL_QUOTA_URL'] = 'http://' + node_ip + ':30091/v1/model-permits'
        runtime.update({'EXECUTION_SANDBOX_ENDPOINT': 'http://rank-sandbox:8080/v1', 'EXECUTION_SANDBOX_API_KEY': sandbox_key,
                        'EXECUTION_SANDBOX_EGRESS_POLICY': json.dumps(policy, separators=(',', ':'))})
    secret('execution-runtime', runtime)
    secret('rank-platform', {key: data[key] for key in ['RANK_ACTION_SECRET', 'RANK_CONTROL_TOKEN']} | {
        'EXECUTION_SERVICE_TOKEN': existing['EXECUTION_SERVICE_TOKEN'], 'RANK_DATABASE_URL': database_url('rank_runtime')})
    secret('rank-control', {'RANK_CONTROL_TOKEN': data['RANK_CONTROL_TOKEN'], 'EXECUTION_SERVICE_TOKEN': existing['EXECUTION_SERVICE_TOKEN']})
    access = {'version': 1, 'operators': [{'subject': 'rank', 'tokenSha256': hashlib.sha256(data['GATEWAY_PASSWORD'].encode()).hexdigest()}]}
    secret('rank-management', {'access.json': json.dumps(access)})
    run(KUBE + ['-n', 'rank-dev', 'apply', '-f', str(HERE / 'base/platform-storage.yaml')])
    name = 'rank-migrate-' + images['platform'].split(':')[1] + '-' + REVISION
    apply_json({'apiVersion': 'batch/v1', 'kind': 'Job', 'metadata': {'name': name, 'namespace': 'rank-dev'}, 'spec': {
        'backoffLimit': 0, 'activeDeadlineSeconds': 120, 'template': {'spec': {'restartPolicy': 'Never', 'automountServiceAccountToken': False,
        'initContainers': [{'name': 'execution-migrate', 'image': images['platform'], 'imagePullPolicy': 'Never', 'command': ['/usr/local/bin/executiond'], 'args': ['-migrate', '-database-timeout=45s'], 'envFrom': [{'secretRef': {'name': 'execution-migration'}}]}],
        'containers': [{'name': 'migrate', 'image': images['platform'], 'imagePullPolicy': 'Never', 'command': ['/usr/local/bin/rankd'], 'args': ['-migrate', '-database-timeout=45s'], 'envFrom': [{'secretRef': {'name': 'rank-migration'}}]}]}}}})
    run(KUBE + ['-n', 'rank-dev', 'wait', '--for=condition=complete', 'job/' + name, '--timeout=60s'])
    grant_runtime()
    # Database migrations finish while all previous application writers are stopped.
    execution = (HERE / 'base/execution.yaml').read_text().replace('rank-execution-lab:build', images['platform'])
    execution = execution.replace("'-worker-timeout=15s'", "'-worker-timeout=60s'").replace("'-rescue-after=25s'", "'-rescue-after=75s'")
    execution = execution.replace('memory: 512Mi', 'memory: 768Mi').replace('sizeLimit: 256Mi', 'sizeLimit: 512Mi')
    if use_sandbox:
        apply_json({'apiVersion': 'v1', 'kind': 'Service', 'metadata': {'name': 'rank-sandbox', 'namespace': 'rank-dev'},
                    'spec': {'ports': [{'name': 'http', 'port': 8080, 'targetPort': 8080}]}})
        apply_json({'apiVersion': 'discovery.k8s.io/v1', 'kind': 'EndpointSlice',
                    'metadata': {'name': 'rank-sandbox', 'namespace': 'rank-dev', 'labels': {'kubernetes.io/service-name': 'rank-sandbox'}},
                    'addressType': 'IPv4', 'ports': [{'name': 'http', 'protocol': 'TCP', 'port': 8080}],
                    'endpoints': [{'addresses': [sandbox_network['serverIP']], 'conditions': {'ready': True}}]})
        execution = execution.replace("'-s3-tls=false'", "'-s3-tls=false', '-executor=opensandbox', '-sandbox-pool=rank-dev', '-sandbox-capacity-cpu=2', '-sandbox-capacity-memory=2Gi', '-sandbox-scope=rank-dev', '-sandbox-image=" + sandbox_image + "'")
    run(KUBE + ['-n', 'rank-dev', 'apply', '-f', '-'], input=execution)
    apply_json({'apiVersion': 'v1', 'kind': 'Service', 'metadata': {'name': 'rank-model-permits', 'namespace': 'rank-dev'},
        'spec': {'type': 'NodePort', 'selector': {'app': 'rank-gateway'}, 'ports': [{'port': 8081, 'targetPort': 8081, 'nodePort': 30091}]}})
    platform = (HERE / 'base/platform.yaml').read_text().replace('rank-platform-lab:build', images['platform']).replace('rank-gateway-lab:build', images['gateway'])
    run(KUBE + ['-n', 'rank-dev', 'apply', '-f', '-'], input=platform)
    for deployment in ['execution-api', 'execution-worker', 'rank-model-fixture', 'rank-api', 'rank-dispatcher', 'rank-control', 'rank-gateway']:
        run(KUBE + ['-n', 'rank-dev', 'rollout', 'status', 'deployment/' + deployment, '--timeout=60s'])
    (STATE / 'platform-mode.json').write_text(json.dumps({'executor': 'opensandbox' if use_sandbox else 'local-process',
        'modelURL': sandbox_network['modelURL'] if use_sandbox else 'http://rank-model-fixture:8800/v1'}, indent=2) + '\n')
    print('Platform ready; gateway credentials remain in', path)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['build', 'up'])
    parser.add_argument('--stage', type=Path)
    parser.add_argument('--sandbox', action='store_true')
    args = parser.parse_args()
    if args.command == 'build':
        if not args.stage:
            parser.error('build requires --stage')
        build(args.stage.resolve())
    else:
        up(args.sandbox)
