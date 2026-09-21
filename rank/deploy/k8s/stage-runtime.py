"""Materialize Rank's declared production Node closure for a Linux lab image."""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import shutil
import tarfile

from lab import ROOT, run


def materialize_links(root):
    # Vendored link overrides and pnpm links must not point into the developer checkout.
    links = sorted((path for path in root.rglob('*') if path.is_symlink()), key=lambda path: len(path.parts))
    for path in links:
        if not path.is_symlink():
            continue
        source = path.resolve(strict=True)
        path.unlink()
        if source.is_dir():
            shutil.copytree(source, path, symlinks=False, ignore=shutil.ignore_patterns('node_modules', 'src', 'tests', '.DS_Store'))
        else:
            shutil.copy2(source, path)
    if any(path.is_symlink() for path in root.rglob('*')):
        raise RuntimeError('Runtime deployment contains unresolved links')


def install_native(runtime, cache, arch):
    modules = runtime / 'node_modules'
    specs = []
    for parent, names in (
        ('sharp', [f'@img/sharp-linux-{arch}', f'@img/sharp-libvips-linux-{arch}']),
        ('koffi', [f'@koromix/koffi-linux-{arch}']),
        ('node-addon-require-builtin', [f'node-addon-require-builtin-linux-{arch}-gnu']),
        ('@deepseek-ai/node-addon-landlock-run', [f'@deepseek-ai/node-addon-landlock-run-linux-{arch}']),
    ):
        manifest = json.loads((modules / parent / 'package.json').read_text())
        for name in names:
            version = manifest['optionalDependencies'][name]
            if version == 'workspace:*':
                platform_manifest = json.loads((ROOT / 'native/landlock-run/packages' / ('linux-' + arch) / 'package.json').read_text())
                if platform_manifest['name'] != name or platform_manifest['version'] != manifest['version']:
                    raise RuntimeError('Landlock entry and platform package versions differ')
                version = platform_manifest['version']
            packed = json.loads(run(['npm', 'pack', '--ignore-scripts', '--json', '--pack-destination', str(cache), name + '@' + version], capture=True))[0]
            archive = cache / packed['filename']
            algorithm, digest = packed['integrity'].split('-', 1)
            actual = base64.b64encode(hashlib.new(algorithm, archive.read_bytes()).digest()).decode()
            if actual != digest:
                raise RuntimeError('Native package integrity mismatch: ' + name)
            destination = modules / name
            destination.mkdir(parents=True, exist_ok=True)
            with tarfile.open(archive) as bundle:
                for member in bundle.getmembers():
                    member.name = member.name.removeprefix('package/')
                    if member.name:
                        bundle.extract(member, destination, filter='data')
            specs.append({'name': name, 'version': version, 'integrity': packed['integrity']})
    return specs


def stage(destination, arch):
    destination.mkdir(parents=True, exist_ok=False)
    runtime = destination / 'runtime'
    try:
        run(['pnpm', '--filter', '@deepseek-ai/dsh-rank-runtime', 'deploy', '--legacy', '--prod',
             '--config.node-linker=hoisted', '--config.auto-install-peers=false', '--config.link-workspace-packages=true', str(runtime)])
    finally:
        # pnpm deploy changes the workspace dependency-selection marker; restore the development selection.
        run(['pnpm', 'install', '--offline', '--ignore-scripts', '--prod=false', '--config.confirmModulesPurge=false'])
    materialize_links(runtime)
    cache = destination / 'native-packages'
    cache.mkdir()
    native = install_native(runtime, cache, arch)
    modules = runtime / 'node_modules'
    for manifest in modules.glob('@deepseek-ai/*/package.json'):
        package = json.loads(manifest.read_text())
        for field in ('dependencies', 'peerDependencies'):
            for name in package.get(field, {}):
                optional = package.get('peerDependenciesMeta', {}).get(name, {}).get('optional', False)
                if name.startswith('@deepseek-ai/') and not optional and not (modules / name / 'package.json').exists():
                    raise RuntimeError(f'Missing runtime dependency: {package["name"]} -> {name}')
    app = modules / '@xyzbit/agentkiln-web'
    for name in ('control-host', 'dist/client'):
        target = app / name
        if target.exists():
            shutil.rmtree(target)
        shutil.copytree(ROOT / 'apps/rank-web' / name, target)
    for name in ('.env', 'auth.json', 'credentials.json', 'settings.yaml'):
        if any(path.is_file() for path in runtime.rglob(name)):
            raise RuntimeError('Refusing a runtime payload containing ' + name)
    record = {'arch': arch, 'native': native, 'runtime': str(runtime)}
    (destination / 'runtime.json').write_text(json.dumps(record, indent=2) + '\n')
    print('Materialized runtime:', runtime)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('destination', type=Path, help='new staging directory outside the checkout')
    parser.add_argument('--arch', choices=['arm64', 'x64'], default='arm64')
    args = parser.parse_args()
    destination = args.destination.resolve()
    if destination.is_relative_to(ROOT):
        parser.error('Stage outside the repository to avoid recursive packaging')
    stage(destination, args.arch)
