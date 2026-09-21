"""Exercise bounded writable storage through the authenticated sandbox command API."""
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


def main():
    auth = lab.credentials()
    token = secrets.token_hex(32)
    image = json.loads((STATE / 'sandbox-image.json').read_text())['image']
    scope = 'rank-storage-' + secrets.token_hex(8)

    def request(path, body=None, method=None, command=False):
        headers = {'OPEN-SANDBOX-API-KEY': auth['apiKey'], 'Content-Type': 'application/json'}
        if command:
            headers['X-EXECD-ACCESS-TOKEN'] = token
        req = urllib.request.Request(lab.ENDPOINT + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
        with urllib.request.urlopen(req, timeout=35) as response:
            return response.read().decode()

    expires = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(seconds=120)
    body = {
        'image': {'uri': image}, 'timeout': 120, 'resourceLimits': {'cpu': '500m', 'memory': '512Mi'},
        'metadata': {'rank.scope': scope}, 'entrypoint': ['/bin/sleep', 'infinity'],
        'env': {'EXECD_ACCESS_TOKEN': token, 'EXECD_BOOTSTRAP_PRE_SCRIPT': '/usr/local/lib/rank-sandbox-init.sh',
                'RANK_RUNTIME_EGRESS_POLICY': '{"allow":[]}',
                'RANK_RUNTIME_EXPIRES_AT': expires.isoformat(), 'RANK_RUNTIME_MAX_LIFETIME': '120s'},
    }
    sid = None
    volume_image = None
    image_container = None
    checks = {}
    try:
        for name, extra in {
            'additionalVolumeRejected': {'volumes': [{'name': 'escape', 'host': {'path': '/rank-no-host-mounts'}, 'mountPath': '/escape'}]},
            'isolationOverlayRejected': {'extensions': {'bootstrap.execd.isolation': 'enable'}},
        }.items():
            try:
                unexpected = json.loads(request('/sandboxes', body | extra))
            except urllib.error.HTTPError as error:
                assert error.code == 400, name
                checks[name] = True
            else:
                request('/sandboxes/' + unexpected['id'], method='DELETE')
                raise AssertionError(name)
        image_container = run(['docker', 'create', '--label', 'rank.scope=' + scope, image, '/bin/true'], capture=True).strip()
        volume_image = run(['docker', 'commit', '--change', 'VOLUME ["/escape"]', image_container], capture=True).strip()
        run(['docker', 'rm', image_container], capture=True)
        image_container = None
        try:
            unexpected = json.loads(request('/sandboxes', body | {'image': {'uri': volume_image}}))
        except urllib.error.HTTPError as error:
            assert error.code == 400, 'Image volume declaration was not rejected'
            checks['imageVolumeRejected'] = True
        else:
            request('/sandboxes/' + unexpected['id'], method='DELETE')
            raise AssertionError('Image declared a writable volume')
        sid = json.loads(request('/sandboxes', body))['id']
        prefix = '/sandboxes/' + sid + '/proxy/44772'
        deadline = time.monotonic() + 10
        while True:
            try:
                request(prefix + '/ping', command=True)
                break
            except urllib.error.HTTPError as error:
                if error.code != 502 or time.monotonic() >= deadline:
                    raise
                time.sleep(0.1)
        containers = run(['docker', 'ps', '-q', '--filter', 'label=rank.scope=' + scope], capture=True).split()
        assert len(containers) == 1
        cid = containers[0]
        host = json.loads(run(['docker', 'inspect', cid, '--format', '{{json .HostConfig}}'], capture=True))
        mounts = json.loads(run(['docker', 'inspect', cid, '--format', '{{json .Mounts}}'], capture=True))
        assert host['ReadonlyRootfs'] and not any(m['Type'] in ('bind', 'volume') for m in mounts)
        assert host['MemorySwap'] == host['Memory'] == 512 * 1024 * 1024
        assert host['LogConfig'] == {'Type': 'local', 'Config': {'max-size': '2m', 'max-file': '2'}}
        checks['readonlyRootWithoutVolumes'] = True
        checks['swapDisabled'] = True
        checks['containerLogRotationConfigured'] = True
        script = r'''
const fs = require('fs');
const assert = require('assert/strict');
assert.equal(process.getuid(), 1000);
const denied = ['/var/tmp', '/home/node', '/opt/rank', '/etc', '/root'].map(dir => {
  try { fs.writeFileSync(dir + '/rank-storage-probe', 'x'); throw new Error('writable root: ' + dir); }
  catch (error) { assert(['EROFS', 'EACCES'].includes(error.code), String(error)); return dir; }
});
const block = Buffer.alloc(1024 * 1024, 120);
function fill(paths, limitMiB) {
  let written = 0, errorCode = null;
  const files = [];
  try {
    for (let i = 0; i < limitMiB + 4; i++) {
      const file = paths[i % paths.length] + '/storage-' + i;
      files.push(file);
      fs.writeFileSync(file, block);
      written++;
    }
  } catch (error) { errorCode = error.code; }
  assert.equal(errorCode, 'ENOSPC');
  assert(written >= limitMiB - 2 && written <= limitMiB, 'unexpected capacity: ' + written);
  for (const file of files) fs.rmSync(file, {force: true});
  const recovered = paths[0] + '/recovered';
  fs.writeFileSync(recovered, block); fs.unlinkSync(recovered);
  return {error: errorCode, recovered: true};
}
const shared = fill(['/run/rank/workspace', '/run/rank/artifacts', '/run/rank/input'], 128);
const temporary = fill(['/tmp'], 32);
const sharedMemory = fill(['/dev/shm'], 16);
const directory = '/run/rank/workspace/inodes';
fs.mkdirSync(directory);
let count = 0, inodeError = null;
try { for (; count < 8300; count++) fs.closeSync(fs.openSync(directory + '/' + count, 'wx')); }
catch (error) { inodeError = error.code; }
assert.equal(inodeError, 'ENOSPC');
assert(count > 8000 && count < 8192);
fs.rmSync(directory, {recursive: true});
fs.writeFileSync('/run/rank/workspace/after-inodes', 'ok');
console.log(JSON.stringify({uid: process.getuid(), denied, shared, temporary, sharedMemory, inodeLimitEnforced: true, usableAfterCleanup: true}));
'''
        frames = [json.loads(line) for line in request(prefix + '/command', {
            'command': 'node -e ' + shlex.quote(script), 'uid': 1000, 'gid': 1000,
            'timeout': 30000, 'envs': {'EXECD_ACCESS_TOKEN': ''},
        }, command=True).splitlines() if line.strip()]
        errors = [frame for frame in frames if frame.get('type') in ('error', 'stderr')]
        assert not errors, errors
        facts = json.loads(''.join(frame['text'] for frame in frames if frame.get('type') == 'stdout'))
        assert facts['usableAfterCleanup']
        checks['nativeProcessLimits'] = facts
    finally:
        if sid:
            request('/sandboxes/' + sid, method='DELETE')
        if image_container:
            run(['docker', 'rm', image_container], capture=True)
        if volume_image:
            run(['docker', 'image', 'rm', volume_image], capture=True)
        remaining = run(['docker', 'ps', '-aq', '--filter', 'label=rank.scope=' + scope], capture=True).split()
        if remaining:
            raise RuntimeError('Storage verifier left owned runtimes behind')
    checks['sandboxDeleted'] = True
    expected = json.loads((HERE / 'fixtures/sandbox-storage.snapshot.json').read_text())
    assert checks == expected, json.dumps(checks)
    report = {'passed': True, 'checks': checks, 'sandboxImage': image,
              'serverImage': run(['docker', 'inspect', lab.NAME, '--format', '{{.Image}}'], capture=True).strip()}
    (STATE / 'sandbox-storage-verification.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
