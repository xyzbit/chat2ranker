"""Build and run the dedicated Docker OpenSandbox experiment beside minikube."""
import argparse
import hashlib
import http.client
import json
import os
import secrets
import shutil
import subprocess
import time
import urllib.error
import urllib.request

from lab import HERE, ROOT, STATE, run

NAME = 'rank-opensandbox-lab'
EXECD = 'sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd@sha256:6cf7dba2f21f0b536e100563d841ac58a9f31c2b0a081b7ac76796a24d6f47e2'
ENDPOINT = 'http://127.0.0.1:18796/v1'


def build_runtime():
    platform = json.loads((STATE / 'platform-images.json').read_text())
    stage = STATE / 'sandbox-image'
    stage.mkdir(parents=True, exist_ok=True)
    base = platform['images']['platform']
    base_id = run(['docker', 'image', 'inspect', base, '--format', '{{.Id}}'], capture=True).strip()
    for name in ['execution-sandbox', 'execution-worker']:
        run(['go', 'build', '-trimpath', '-o', str(stage / name), './cmd/' + name], cwd=ROOT / 'execution/backend',
            env=dict(os.environ, GOOS='linux', GOARCH=platform['arch'], CGO_ENABLED='0', GOTOOLCHAIN='go1.26.0'))
    digest = hashlib.sha256((base_id + EXECD).encode())
    for name in ['sandbox.Dockerfile', 'sandbox-init.sh']:
        shutil.copy2(HERE / name, stage / name)
    for name in ['sandbox.Dockerfile', 'sandbox-init.sh', 'execution-sandbox', 'execution-worker']:
        digest.update((stage / name).read_bytes())
    tag = 'rank-sandbox-lab:' + digest.hexdigest()[:16]
    run(['docker', 'build', '--provenance=false', '--pull=false', '--platform=linux/' + platform['arch'],
         '-f', str(stage / 'sandbox.Dockerfile'), '--build-arg', 'RANK_PLATFORM_IMAGE=' + base,
         '--build-arg', 'EXECD_IMAGE=' + EXECD, '-t', tag, str(stage)])
    image = run(['docker', 'image', 'inspect', tag, '--format', '{{.Id}}'], capture=True).strip()
    (STATE / 'sandbox-image.json').write_text(json.dumps({'tag': tag, 'image': image, 'platform': base_id}, indent=2) + '\n')
    print('Sandbox image:', image)


def build_server(index):
    build = ['docker', 'build', '--pull=false']
    if os.environ.get('DOCKER_BUILDKIT') != '0':
        build.append('--provenance=false')
    if subprocess.run(['docker', 'image', 'inspect', 'rank-opensandbox-upstream:0.2.3'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
        run(build + ['-f', str(HERE / 'opensandbox-upstream.Dockerfile'), '--build-arg', 'PYPI_INDEX=' + index,
                     '-t', 'rank-opensandbox-upstream:0.2.3', str(HERE)])
    run(build + ['-f', str(HERE / 'opensandbox-server.Dockerfile'), '-t', 'rank-opensandbox-server:0.2.3', str(HERE)])
    record = {'version': '0.2.3', 'patchSha256': hashlib.sha256((HERE / 'opensandbox-proxy-only.py').read_bytes()).hexdigest(),
              'images': {name: run(['docker', 'image', 'inspect', name + ':0.2.3', '--format', '{{.Id}}'], capture=True).strip()
                         for name in ['rank-opensandbox-upstream', 'rank-opensandbox-server']}}
    (STATE / 'opensandbox-server-image.json').write_text(json.dumps(record, indent=2) + '\n')


def credentials():
    path = STATE / 'opensandbox-lab.json'
    if not path.exists():
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as file:
            json.dump({'apiKey': secrets.token_hex(32)}, file)
    return json.loads(path.read_text())


def up(network=None):
    network_file = STATE / 'sandbox-network.json'
    if network is None:
        network = json.loads(network_file.read_text())['network'] if network_file.exists() else 'bridge'
    auth = credentials()
    directory = STATE / 'opensandbox-server'
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    config = f'''[server]
host = "0.0.0.0"
port = 8080
api_key = {json.dumps(auth['apiKey'])}
max_sandbox_timeout_seconds = 300
[runtime]
type = "docker"
execd_image = "{EXECD}"
[docker]
network_mode = "{network}"
host_ip = "127.0.0.1"
no_new_privileges = true
pids_limit = 128
publish_ports = false
rank_readonly_runtime = true
rank_workspace_mib = 128
rank_tmp_mib = 32
rank_shm_mib = 16
rank_tmpfs_inodes = 8192
rank_log_mib = 2
port_range_min = 42200
port_range_max = 42300
[renew_intent]
enabled = false
[store]
type = "sqlite"
path = "/state/server.db"
[storage]
allowed_host_paths = ["/rank-no-host-mounts"]
'''
    path = directory / 'server.toml'
    changed = path.exists() and path.read_text() != config
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), 'w') as file:
        file.write(config)
    existing = run(['docker', 'ps', '-aq', '--filter', 'name=^/' + NAME + '$'], capture=True).strip()
    if existing:
        label = run(['docker', 'inspect', existing, '--format', '{{index .Config.Labels "rank.lab"}}'], capture=True).strip()
        if label != NAME:
            raise RuntimeError('Container name is owned by another application')
        connect_network(NAME, network)
        run(['docker', 'restart' if changed else 'start', NAME])
    else:
        socket = run(['docker', 'context', 'inspect', '--format', '{{.Endpoints.docker.Host}}'], capture=True).strip()
        if not socket.startswith('unix://'):
            raise RuntimeError('This local experiment requires a Unix Docker socket')
        run(['docker', 'run', '-d', '--name', NAME, '--label', 'rank.lab=' + NAME,
             '--network', network,
             '-p', '127.0.0.1:18796:8080', '--mount', 'type=bind,src=' + socket[7:] + ',dst=/var/run/docker.sock',
             '--mount', 'type=bind,src=' + str(directory) + ',dst=/state',
             'rank-opensandbox-server:0.2.3'])
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        request = urllib.request.Request(ENDPOINT + '/sandboxes', headers={'OPEN-SANDBOX-API-KEY': auth['apiKey']})
        try:
            with urllib.request.urlopen(request, timeout=2) as response:
                if response.status == 200:
                    start_model(network)
                    server_ip = network_address(NAME, network)
                    model_ip = network_address('rank-opensandbox-model', network)
                    policy_path = STATE / 'sandbox-egress-policy.json'
                    if not policy_path.exists():
                        network_config = json.loads(run(['docker', 'network', 'inspect', network], capture=True))[0]
                        subnet = next(item['Subnet'] for item in network_config['IPAM']['Config'] if ':' not in item['Subnet'])
                        policy_path.write_text(json.dumps({'allow': [{'protocol': 'tcp', 'cidr': subnet, 'port': 8800}]}, indent=2) + '\n')
                    (STATE / 'sandbox-network.json').write_text(json.dumps({'network': network, 'serverIP': server_ip,
                        'modelURL': 'http://' + model_ip + ':8800/v1'}, indent=2) + '\n')
                    print('OpenSandbox ready:', ENDPOINT)
                    return
        except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.RemoteDisconnected):
            time.sleep(0.2)
    raise RuntimeError('OpenSandbox did not become ready; inspect the dedicated container logs')


def network_address(name, network):
    networks = json.loads(run(['docker', 'inspect', name, '--format', '{{json .NetworkSettings.Networks}}'], capture=True))
    return networks[network]['IPAddress']


def connect_network(name, network):
    networks = json.loads(run(['docker', 'inspect', name, '--format', '{{json .NetworkSettings.Networks}}'], capture=True))
    if network not in networks:
        run(['docker', 'network', 'connect', network, name])


def start_model(network):
    name = 'rank-opensandbox-model'
    existing = run(['docker', 'ps', '-aq', '--filter', 'name=^/' + name + '$'], capture=True).strip()
    if existing:
        label = run(['docker', 'inspect', existing, '--format', '{{index .Config.Labels "rank.lab"}}'], capture=True).strip()
        if label != NAME:
            raise RuntimeError('Model container name is owned by another application')
        connect_network(name, network)
        run(['docker', 'start', name])
    else:
        image = json.loads((STATE / 'platform-images.json').read_text())['images']['platform']
        run(['docker', 'run', '-d', '--name', name, '--label', 'rank.lab=' + NAME,
             '--network', network,
             '--entrypoint', 'node', image, '/opt/rank/fixtures/model.mjs'])


def verify():
    auth = credentials()
    image = json.loads((STATE / 'sandbox-image.json').read_text())['image']
    network = json.loads((STATE / 'sandbox-network.json').read_text())
    env = dict(os.environ, GOTOOLCHAIN='go1.26.0', EXECUTION_TEST_SANDBOX_URL=ENDPOINT,
               EXECUTION_TEST_SANDBOX_API_KEY=auth['apiKey'], EXECUTION_TEST_SANDBOX_IMAGE=image,
               EXECUTION_TEST_SANDBOX_MODEL_URL=network['modelURL'],
               EXECUTION_SANDBOX_EGRESS_POLICY=(STATE / 'sandbox-egress-policy.json').read_text())
    run(['go', 'test', './internal/executor', '-run', 'TestOpenSandbox', '-v', '-count=1'], cwd=ROOT / 'execution/backend', env=env)
    run(['node', 'rank/examples/execution-lifecycle/verify-remote.mjs'], env=env)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['build-runtime', 'build-server', 'up', 'verify'])
    parser.add_argument('--pypi-index', default='https://pypi.org/simple')
    parser.add_argument('--network', choices=['bridge', 'rank-dev'])
    args = parser.parse_args()
    if args.command == 'build-runtime':
        build_runtime()
    elif args.command == 'build-server':
        build_server(args.pypi_index)
    elif args.command == 'up':
        up(args.network)
    else:
        verify()
