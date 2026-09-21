"""Verify protected sandbox expiry while the dedicated lifecycle server is stopped."""
import datetime
import importlib.util
import json
import secrets
import shlex
import time
import urllib.error
import urllib.request

from lab import HERE, STATE, run

spec = importlib.util.spec_from_file_location('sandbox_lab', HERE / 'sandbox-lab.py')
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)
auth = lab.credentials()
token = secrets.token_hex(32)
image = json.loads((STATE / 'sandbox-image.json').read_text())['image']
network = json.loads((STATE / 'sandbox-network.json').read_text())['network']


def request(path, body=None, method=None, *, command=False):
    headers = {'OPEN-SANDBOX-API-KEY': auth['apiKey'], 'Content-Type': 'application/json'}
    if command:
        headers['X-EXECD-ACCESS-TOKEN'] = token
    req = urllib.request.Request(lab.ENDPOINT + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=10) as response:
        return response.read().decode()


sid = None
server_stopped = False
try:
    expires = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(seconds=25)
    created = json.loads(request('/sandboxes', {
        'image': {'uri': image}, 'timeout': 90, 'resourceLimits': {'cpu': '500m', 'memory': '512Mi'},
        'metadata': {'rank.scope': 'rank-lifetime-test'}, 'entrypoint': ['/bin/sleep', 'infinity'],
        'env': {'EXECD_ACCESS_TOKEN': token, 'EXECD_BOOTSTRAP_PRE_SCRIPT': '/usr/local/lib/rank-sandbox-init.sh',
                'RANK_RUNTIME_EGRESS_POLICY': '{"allow":[]}',
                'RANK_RUNTIME_EXPIRES_AT': expires.isoformat(), 'RANK_RUNTIME_MAX_LIFETIME': '25s'},
    }))
    sid = created['id']
    prefix = '/sandboxes/' + sid + '/proxy/44772'
    ready_deadline = time.monotonic() + 5
    while True:
        try:
            request(prefix + '/ping', command=True)
            break
        except urllib.error.HTTPError as error:
            if error.code != 502 or time.monotonic() >= ready_deadline:
                raise
            time.sleep(0.1)
    script = '''const fs = require('fs');
let rootEnvironmentReadable = false, canSignalRoot = false;
try { fs.readFileSync('/proc/1/environ'); rootEnvironmentReadable = true; } catch {}
try { process.kill(1, 0); canSignalRoot = true; } catch {}
const facts = { uid: process.getuid(), tokenVisible: Boolean(process.env.EXECD_ACCESS_TOKEN), rootEnvironmentReadable, canSignalRoot, dockerSocketVisible: fs.existsSync('/var/run/docker.sock') };
require('child_process').spawn('/usr/bin/setsid', ['/bin/sh', '-c', 'trap "" TERM; while :; do date +%s > /run/rank/workspace/heartbeat; sleep 1; done'], { detached: true, stdio: 'ignore' }).unref();
console.log(JSON.stringify(facts));'''
    output = request(prefix + '/command', {'command': 'node -e ' + shlex.quote(script), 'uid': 1000, 'gid': 1000,
        'timeout': 5000, 'envs': {'EXECD_ACCESS_TOKEN': ''}}, command=True)
    frames = [json.loads(line) for line in output.splitlines() if line.strip()]
    facts = next(json.loads(frame['text']) for frame in frames if frame.get('type') == 'stdout')
    assert facts == {'uid': 1000, 'tokenVisible': False, 'rootEnvironmentReadable': False, 'canSignalRoot': False, 'dockerSocketVisible': False}, facts
    containers = run(['docker', 'ps', '-q', '--filter', 'label=rank.scope=rank-lifetime-test'], capture=True).split()
    assert len(containers) == 1, 'Run this experiment without concurrent lifetime tests'
    cid = containers[0]
    # Reading the independently updated file proves the detached workload survived command completion.
    heartbeat = run(['docker', 'exec', cid, 'cat', '/run/rank/workspace/heartbeat'], capture=True).strip()
    assert heartbeat.isdigit()
    run(['docker', 'kill', lab.NAME])
    server_stopped = True
    initial = run(['docker', 'inspect', cid, '--format', '{{.State.Running}}'], capture=True).strip()
    assert initial == 'true', 'Sandbox stopped before independent expiry was tested'
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        state = json.loads(run(['docker', 'inspect', cid, '--format', '{{json .State}}'], capture=True))
        if not state['Running']:
            break
        time.sleep(0.25)
    else:
        raise RuntimeError('Sandbox outlived its independently enforced deadline')
    stopped_at = datetime.datetime.fromisoformat(state['FinishedAt'].replace('Z', '+00:00'))
    assert abs((stopped_at - expires).total_seconds()) < 5, 'Shutdown did not match the protected deadline'
    report = {'sandboxImage': image, 'serverImage': run(['docker', 'inspect', lab.NAME, '--format', '{{.Image}}'], capture=True).strip(),
              'facts': facts, 'detachedWorkloadObserved': True, 'lifecycleServerKilled': True,
              'providerTTLSeconds': 90, 'protectedLifetimeSeconds': 25, 'stoppedAtDeadline': True, 'exitCode': state['ExitCode']}
    (STATE / 'sandbox-lifetime-verification.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
finally:
    if server_stopped:
        lab.up(network)
    if sid:
        try:
            request('/sandboxes/' + sid, method='DELETE')
        except urllib.error.HTTPError as error:
            if error.code != 404:
                raise
