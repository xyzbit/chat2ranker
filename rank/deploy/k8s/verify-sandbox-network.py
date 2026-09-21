"""Verify enforced runtime egress against reachable local TCP/UDP targets."""
import datetime
import importlib.util
import json
import os
import secrets
import shlex
import subprocess
import time
import urllib.error
import urllib.request

from lab import HERE, STATE, run

spec = importlib.util.spec_from_file_location('sandbox_lab', HERE / 'sandbox-lab.py')
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)


def main():
    auth = lab.credentials()
    token = secrets.token_hex(32)
    image = json.loads((STATE / 'sandbox-image.json').read_text())['image']
    platform = json.loads((STATE / 'platform-images.json').read_text())['images']['platform']
    network = json.loads((STATE / 'sandbox-network.json').read_text())['network']
    scope = 'rank-network-' + secrets.token_hex(8)
    owned = []
    sid = None
    server_stopped = False
    facts = {}

    def request(path, body=None, method=None):
        headers = {'OPEN-SANDBOX-API-KEY': auth['apiKey'], 'X-EXECD-ACCESS-TOKEN': token, 'Content-Type': 'application/json'}
        req = urllib.request.Request(lab.ENDPOINT + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
        with urllib.request.urlopen(req, timeout=20) as response:
            return response.read().decode()

    def node(container, script):
        return run(['docker', 'exec', '--user', '1000:1000', container, 'node', '-e', script], capture=True)

    def create_target(index):
        name = scope + '-target-' + str(index)
        run(['docker', 'run', '-d', '--name', name, '--label', 'rank.lab=' + scope, '--network', network,
             '--cpus=0.5', '--memory=96m', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges',
             '--user=1000:1000', '--mount', 'type=bind,src=' + str(HERE / 'fixtures/network-target.mjs') + ',dst=/target.mjs,readonly',
             '--entrypoint', 'node', platform, '/target.mjs'], capture=True)
        owned.append(name)
        return name, lab.network_address(name, network)

    script = r'''
const net = require('net'), http = require('http'), dgram = require('dgram'), fs = require('fs'), cp = require('child_process');
async function tcp(host, port, path) {
  return new Promise(resolve => {
    const request = http.get({host, port, path}, response => { response.resume(); response.on('end', () => resolve(true)); });
    const timer = setTimeout(() => { request.destroy(); resolve(false); }, 700);
    request.on('close', () => clearTimeout(timer)); request.on('error', () => resolve(false));
  });
}
async function udp(host, path) {
  return new Promise(resolve => {
    const socket = dgram.createSocket('udp4');
    let done = false;
    const finish = value => { if (done) return; done = true; clearTimeout(timer); socket.close(); resolve(value); };
    const timer = setTimeout(() => finish(false), 700);
    socket.on('message', () => finish(true)); socket.on('error', () => finish(false));
    socket.send(path, 8802, host);
  });
}
'''
    try:
        first, first_ip = create_target(1)
        second, second_ip = create_target(2)
        probe = script + f'''(async () => {{ console.log(JSON.stringify({{
tcp: await tcp({json.dumps(second_ip)}, 8800, '/control'),
port: await tcp({json.dumps(second_ip)}, 8801, '/control'),
udp: await udp({json.dumps(second_ip)}, '/control')
}})); }})().catch(error => {{ console.error(error); process.exit(1); }});'''
        deadline = time.monotonic() + 10
        while True:
            controls = json.loads(node(first, probe))
            if all(controls.values()):
                break
            if time.monotonic() > deadline:
                raise AssertionError('Control targets are not all reachable')
        facts['listeningTargetsReachableOutsideSandbox'] = controls
        assert all(json.loads(node(second, probe.replace(json.dumps(second_ip), json.dumps(first_ip)))).values())
        rejected = {}
        for label, policy in [('missing-network-capability', '{"allow":[]}'), ('missing-policy', '')]:
            name = scope + '-' + label
            raw_env = dict(os.environ, EXECD_ACCESS_TOKEN='private-fixture-token',
                           RANK_RUNTIME_EXPIRES_AT=(datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(seconds=30)).isoformat(),
                           RANK_RUNTIME_MAX_LIFETIME='30s', RANK_RUNTIME_EGRESS_POLICY=policy)
            run(['docker', 'run', '-d', '--name', name, '--label', 'rank.lab=' + scope, '--network', network,
                 '--read-only', '--memory=128m', '--cpus=0.5', '--cap-drop=NET_ADMIN', '--security-opt=no-new-privileges',
                 '-e', 'EXECD_ACCESS_TOKEN', '-e', 'RANK_RUNTIME_EXPIRES_AT', '-e', 'RANK_RUNTIME_MAX_LIFETIME', '-e', 'RANK_RUNTIME_EGRESS_POLICY',
                 '--entrypoint', '/usr/local/lib/rank-sandbox-init.sh', image], capture=True, env=raw_env)
            owned.append(name)
            deadline = time.monotonic() + 10
            while True:
                status = json.loads(run(['docker', 'inspect', name, '--format', '{{json .State}}'], capture=True))
                if not status['Running']:
                    break
                if time.monotonic() > deadline:
                    raise AssertionError('Invalid network setup left a runtime running')
                time.sleep(0.1)
            assert status['ExitCode'] != 0
            logs = subprocess.run(['docker', 'logs', name], text=True, capture_output=True, check=True)
            expected_error = 'network policy installation failed' if label == 'missing-network-capability' else 'requires an explicit egress policy'
            assert expected_error in logs.stdout + logs.stderr, 'Runtime failed for an unrelated reason'
            rejected[label] = True
        facts['invalidSetupStopsBeforeDaemon'] = rejected
        expires = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(seconds=90)
        body = {
            'image': {'uri': image}, 'timeout': 120, 'resourceLimits': {'cpu': '500m', 'memory': '512Mi'},
            'metadata': {'rank.scope': scope}, 'entrypoint': ['/bin/sleep', 'infinity'],
            'env': {'EXECD_ACCESS_TOKEN': token, 'RANK_RUNTIME_EXPIRES_AT': expires.isoformat(), 'RANK_RUNTIME_MAX_LIFETIME': '90s',
                    'RANK_RUNTIME_EGRESS_POLICY': json.dumps({'allow': [{'protocol': 'tcp', 'cidr': second_ip + '/32', 'port': 8800}]})},
        }
        sid = json.loads(request('/sandboxes', body))['id']
        prefix = '/sandboxes/' + sid + '/proxy/44772'
        deadline = time.monotonic() + 10
        while True:
            try:
                request(prefix + '/ping')
                break
            except urllib.error.HTTPError as error:
                if error.code != 502 or time.monotonic() > deadline:
                    raise
                time.sleep(0.1)
        cid = 'sandbox-' + sid
        workload = script + f'''(async () => {{
const bounding = name => BigInt('0x' + fs.readFileSync('/proc/' + name + '/status','utf8').match(/^CapBnd:\\s*(\\w+)/m)[1]);
const forbidden = (1n << 12n) | (1n << 13n);
const daemon = fs.readdirSync('/proc').filter(name => /^\\d+$/.test(name)).find(name => {{ try {{ return fs.readFileSync('/proc/' + name + '/comm','utf8').trim() === 'execd'; }} catch {{ return false; }} }});
if (!daemon) throw new Error('execd process was not found');
const mutation = cp.spawnSync('/usr/sbin/iptables', ['-F','OUTPUT'], {{encoding:'utf8'}});
console.log(JSON.stringify({{
allowed: await tcp({json.dumps(second_ip)}, 8800, '/allowed'),
otherPort: await tcp({json.dumps(second_ip)}, 8801, '/denied-port'),
otherAddress: await tcp({json.dumps(first_ip)}, 8800, '/denied-address'),
udp: await udp({json.dumps(second_ip)}, '/denied-udp'),
rootNetworkCapabilitiesDropped: (bounding('1') & forbidden) === 0n,
daemonNetworkCapabilitiesDropped: (bounding(daemon) & forbidden) === 0n,
childNetworkCapabilitiesDropped: (bounding('self') & forbidden) === 0n,
cannotChangePolicy: mutation.status !== 0
}})); }})().catch(error => {{ console.error(error); process.exit(1); }});'''
        frames = [json.loads(line) for line in request(prefix + '/command', {
            'command': 'node -e ' + shlex.quote(workload), 'uid': 1000, 'gid': 1000, 'timeout': 15000,
            'envs': {'EXECD_ACCESS_TOKEN': ''},
        }).splitlines() if line.strip()]
        assert not any(frame.get('type') in ('error', 'stderr') for frame in frames), frames
        assert any(frame.get('type') == 'execution_complete' for frame in frames)
        facts['runtime'] = json.loads(''.join(frame['text'] for frame in frames if frame.get('type') == 'stdout'))
        for binary in ['iptables', 'ip6tables']:
            # Docker control is privileged infrastructure; the daemon/workload
            # capability assertions above inspect the actual application tree.
            rules = run(['docker', 'exec', '--user', '0', cid, '/usr/sbin/' + binary, '-S'], capture=True)
            assert '-P OUTPUT DROP' in rules and '-P FORWARD DROP' in rules
        facts['ipv4AndIpv6DefaultDenyInstalled'] = True
        run(['docker', 'kill', lab.NAME], capture=True)
        server_stopped = True
        after = json.loads(node(cid, probe))
        facts['afterLifecycleLoss'] = after
        arrivals = json.loads(node(second, "fetch('http://127.0.0.1:8800/facts').then(r=>r.json()).then(v=>console.log(JSON.stringify(v)))"))
        first_arrivals = json.loads(node(first, "fetch('http://127.0.0.1:8800/facts').then(r=>r.json()).then(v=>console.log(JSON.stringify(v)))"))
        assert any(item['path'] == '/allowed' for item in arrivals)
        assert not any(item['path'].startswith('/denied') for item in arrivals + first_arrivals)
        facts['deniedTrafficAbsentAtTargets'] = True
    finally:
        if server_stopped:
            lab.up(network)
        if sid:
            request('/sandboxes/' + sid, method='DELETE')
        for name in reversed(owned):
            run(['docker', 'rm', '-f', name], capture=True)
    facts['resourcesRemoved'] = not run(['docker', 'ps', '-aq', '--filter', 'label=rank.scope=' + scope], capture=True).strip()
    expected = json.loads((HERE / 'fixtures/sandbox-network.snapshot.json').read_text())
    assert facts == expected, json.dumps(facts)
    report = {'passed': True, 'checks': facts, 'sandboxImage': image,
              'serverImage': run(['docker', 'inspect', lab.NAME, '--format', '{{.Image}}'], capture=True).strip()}
    (STATE / 'sandbox-network-verification.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
