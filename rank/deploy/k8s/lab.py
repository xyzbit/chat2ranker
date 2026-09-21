"""Build and deploy the trusted Execution experiment in the dedicated rank-dev cluster."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import platform
import secrets
import shutil
import subprocess
import sys
import tarfile
import urllib.request

if sys.version_info < (3, 12):
    raise SystemExit("The Execution lab requires Python 3.12 or newer; use python3.12 on macOS.")


HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
STATE = Path.home() / ".local/state/agentkiln/rank-k8s"
KUBE = ["kubectl", "--kubeconfig=" + str(STATE / "config"), "--context=rank-dev"]
NODE_IMAGE = "public.ecr.aws/docker/library/node@sha256:2fe369e969550cde8e867afc3fe370b260140cab4a23d467074295b42163d553"
POSTGRES_IMAGE = "postgres@sha256:b3968e348b48f1198cc6de6611d055dbad91cd561b7990c406c3fc28d7095b21"
POSTGRES_LAB_IMAGE = "rank-postgres-lab:b3968e348b48f119"


def run(args, *, capture=False, input=None, env=None, cwd=ROOT):
    return subprocess.run(args, cwd=cwd, env=env, input=input, text=True, check=True,
                          stdout=subprocess.PIPE if capture else None).stdout


def build(arch):
    context = STATE / "build" / arch
    context.mkdir(parents=True, exist_ok=True)
    environment = dict(os.environ, GOOS="linux", GOARCH=arch, CGO_ENABLED="0", GOTOOLCHAIN="go1.26.0")
    for binary in ("executiond", "execution-worker"):
        run(["go", "build", "-trimpath", "-o", str(context / binary), "./cmd/" + binary],
            cwd=ROOT / "execution/backend", env=environment)
    shutil.copy(ROOT / "rank/examples/execution-lifecycle/harness.mjs", context / "harness.mjs")
    for name in ("execution-lab.Dockerfile", "object-lab.Dockerfile"):
        shutil.copy(HERE / name, context / name)
    archive = context / ("seaweedfs-4.47-" + arch + ".tar.gz")
    source = "https://github.com/seaweedfs/seaweedfs/releases/download/4.47/linux_" + arch + ".tar.gz"
    if not archive.exists():
        partial = archive.with_suffix(".partial")
        with urllib.request.urlopen(source, timeout=30) as response, partial.open("wb") as output:
            shutil.copyfileobj(response, output)
        expected = urllib.request.urlopen(source + ".md5", timeout=30).read().decode().split()[0]
        if hashlib.md5(partial.read_bytes()).hexdigest() != expected:
            raise RuntimeError("SeaweedFS release checksum mismatch")
        partial.rename(archive)
    with tarfile.open(archive) as bundle:
        member = next(item for item in bundle if Path(item.name).name == "weed" and item.isfile())
        with bundle.extractfile(member) as source_file, (context / "weed").open("wb") as output:
            shutil.copyfileobj(source_file, output)
    (context / "weed").chmod(0o755)
    execution_hash = hashlib.sha256()
    for name in ("executiond", "execution-worker", "harness.mjs", "execution-lab.Dockerfile"):
        execution_hash.update((context / name).read_bytes())
    object_hash = hashlib.sha256((context / "weed").read_bytes() + (context / "object-lab.Dockerfile").read_bytes()).hexdigest()
    try:
        run(["docker", "image", "inspect", POSTGRES_IMAGE], capture=True)
    except subprocess.CalledProcessError:
        run(["docker", "pull", POSTGRES_IMAGE])
    run(["docker", "tag", POSTGRES_IMAGE, POSTGRES_LAB_IMAGE])
    images = {"execution": "rank-execution-lab:" + execution_hash.hexdigest()[:16],
              "objects": "rank-object-lab:" + object_hash[:16], "postgres": POSTGRES_LAB_IMAGE}
    run(["docker", "pull", NODE_IMAGE])
    for kind, filename in (("execution", "execution-lab.Dockerfile"), ("objects", "object-lab.Dockerfile")):
        run(["docker", "build", "--provenance=false", "--platform=linux/" + arch, "-f", str(context / filename),
             "-t", images[kind], str(context)])
    record = {"arch": arch, "images": images, "postgresSource": POSTGRES_IMAGE,
              "revision": run(["git", "rev-parse", "HEAD"], capture=True).strip(),
              "digests": {name: run(["docker", "image", "inspect", image, "--format", "{{.Id}}"], capture=True).strip()
                          for name, image in images.items()}}
    (STATE / "images.json").write_text(json.dumps(record, indent=2) + "\n")
    print("Built fixture images; image identities saved to", STATE / "images.json")


def apply_json(value):
    run(KUBE + ["apply", "-f", "-"], input=json.dumps(value))


def up():
    from database_roles import REVISION, database_url, provision, grant_runtime
    if run(KUBE + ['-n', 'rank-dev', 'get', 'deployment/rank-api', '--ignore-not-found', '-o', 'name'], capture=True).strip():
        raise RuntimeError('Use platform-lab.py up for an installed complete platform')
    run(KUBE + ["wait", "--for=condition=Ready", "node/rank-dev", "--timeout=60s"])
    record = json.loads((STATE / "images.json").read_text())
    for image in record["images"].values():
        run(["minikube", "-p", "rank-dev", "image", "load", "--daemon", image], env=dict(os.environ, KUBECONFIG=str(STATE / "config")))
    secret_path = STATE / "execution-lab.json"
    if not secret_path.exists():
        data = {"POSTGRES_PASSWORD": secrets.token_hex(32), "EXECUTION_SERVICE_TOKEN": secrets.token_hex(32),
                "EXECUTION_CREDENTIAL_KEY": base64.b64encode(secrets.token_bytes(32)).decode(),
                "EXECUTION_S3_ACCESS_KEY": secrets.token_hex(16), "EXECUTION_S3_SECRET_KEY": secrets.token_hex(32)}
        with os.fdopen(os.open(secret_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as file:
            json.dump(data, file)
    data = json.loads(secret_path.read_text())
    run(KUBE + ["apply", "-f", str(HERE / "base/namespace.yaml")])
    apply_json({"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "execution-lab", "namespace": "rank-dev"}, "stringData": data})
    for name in ['execution-api', 'execution-worker']:
        if run(KUBE + ['-n', 'rank-dev', 'get', 'deployment/' + name, '--ignore-not-found', '-o', 'name'], capture=True).strip():
            run(KUBE + ['-n', 'rank-dev', 'scale', 'deployment/' + name, '--replicas=0'])
            run(KUBE + ['-n', 'rank-dev', 'wait', '--for=delete', 'pod', '-l', 'app=' + name, '--timeout=90s'])
    runtime = {name: value for name, value in data.items() if name != "POSTGRES_PASSWORD"}
    runtime["EXECUTION_DATABASE_URL"] = database_url("execution_runtime")
    runtime["EXECUTION_WORKER_VERSION"] = record["images"]["execution"].split(":")[1]
    apply_json({"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "execution-runtime", "namespace": "rank-dev"}, "stringData": runtime})
    for name in ("postgres", "objects"):
        manifest = (HERE / ("base/" + name + ".yaml")).read_text().replace("rank-object-lab:build", record["images"]["objects"]).replace("rank-postgres-lab:build", record["images"]["postgres"])
        run(KUBE + ["-n", "rank-dev", "apply", "-f", "-"], input=manifest)
        run(KUBE + ["-n", "rank-dev", "rollout", "status", "deployment/" + name, "--timeout=60s"])
    provision()
    job_name = "execution-migrate-" + record["images"]["execution"].split(":")[1] + "-" + REVISION
    job = {"apiVersion": "batch/v1", "kind": "Job", "metadata": {"name": job_name, "namespace": "rank-dev"},
           "spec": {"backoffLimit": 1, "activeDeadlineSeconds": 120, "template": {"spec": {
               "restartPolicy": "Never", "automountServiceAccountToken": False,
               "securityContext": {"runAsNonRoot": True, "runAsUser": 1000},
               "containers": [{"name": "migrate", "image": record["images"]["execution"], "imagePullPolicy": "Never",
                               "args": ["-migrate", "-database-timeout=45s"], "envFrom": [{"secretRef": {"name": "execution-migration"}}]}]}}}}
    apply_json(job)
    run(KUBE + ["-n", "rank-dev", "wait", "--for=condition=complete", "job/" + job_name, "--timeout=60s"])
    grant_runtime()
    manifest = (HERE / "base/execution.yaml").read_text().replace("rank-execution-lab:build", record["images"]["execution"])
    run(KUBE + ["-n", "rank-dev", "apply", "-f", "-"], input=manifest)
    for name in ("execution-api", "execution-worker"):
        run(KUBE + ["-n", "rank-dev", "rollout", "status", "deployment/" + name, "--timeout=60s"])


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["build", "up", "status"])
    parser.add_argument("--arch", choices=["arm64", "amd64"], default="arm64" if platform.machine() == "arm64" else "amd64")
    args = parser.parse_args()
    STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    if args.action == "build":
        build(args.arch)
    elif args.action == "up":
        up()
    else:
        run(KUBE + ["-n", "rank-dev", "get", "pods,services,jobs,pvc"])
