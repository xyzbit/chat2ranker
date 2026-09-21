"""Install bounded KEDA scaling only in the dedicated rank-dev experiment."""
import argparse
import json
import os
import secrets
import tarfile

from lab import HERE, KUBE, STATE, apply_json, run

KEDA_VERSION = "2.20.2"


# Official multi-platform manifests, verified against ghcr.io for this release.
KEDA_IMAGES = {
    "keda": "sha256:fe74c7b8849586a67ad2201bcb89e7f5ac221ff90399ecaa8fd28427f1ef11e6",
    "keda-metrics-apiserver": "sha256:27286536a8a775aeeee37a7e343f8ecebb27ebf680ee1181a4f99e82eefb253b",
    "keda-admission-webhooks": "sha256:41f74102aba7959c6e8d08b433ab8a5fd6cae7c5646c78f7fe3de40a52df3439",
}


def install(mirror=None):
    record = {"version": KEDA_VERSION, "source": mirror or "ghcr.io", "images": {}}
    for name, digest in KEDA_IMAGES.items():
        image = "rank-" + name + "-lab:" + KEDA_VERSION
        if mirror:
            archive = STATE / (name + "-" + KEDA_VERSION + "-verified.tar")
            source = mirror + "/kedacore/" + name + "@" + digest
            run(["go", "run", "github.com/google/go-containerregistry/cmd/crane@v0.20.3", "pull", "--platform", "linux/" + json.loads((STATE / "platform-images.json").read_text())["arch"], source, str(archive)], env=dict(os.environ, GOTOOLCHAIN="go1.26.0"))
            run(["docker", "load", "-i", str(archive)])
            with tarfile.open(archive) as bundle:
                reference = json.load(bundle.extractfile("manifest.json"))[0]["RepoTags"][0]
            run(["docker", "tag", reference, image])
        else:
            source = "ghcr.io/kedacore/" + name + "@" + digest
            run(["docker", "pull", source])
            run(["docker", "tag", source, image])
        run(["minikube", "-p", "rank-dev", "image", "load", "--daemon", image], env=dict(os.environ, KUBECONFIG=str(STATE / "config")))
        record["images"][name] = {"manifest": digest, "imageId": run(["docker", "image", "inspect", image, "--format", "{{.Id}}"], capture=True).strip()}
    (STATE / "keda-images.json").write_text(json.dumps(record, indent=2) + "\n")
    run(["helm", "--kubeconfig=" + str(STATE / "config"), "--kube-context=rank-dev", "upgrade", "--install",
         "rank-keda", "keda", "--repo", "https://kedacore.github.io/charts", "--version", KEDA_VERSION,
         "--namespace", "rank-keda", "--create-namespace", "--values", str(HERE / "base/keda-values.yaml"),
         "--wait", "--timeout", "60s"])


def enable():
    deployments = json.loads(run(KUBE + ["-n", "rank-dev", "get", "deployment/execution-api", "deployment/execution-worker", "-o", "json"], capture=True))["items"]
    configurations = []
    for deployment in deployments:
        args = deployment["spec"]["template"]["spec"]["containers"][0]["args"]
        configurations.append({key: next((arg.split("=", 1)[1] for arg in args if arg.startswith("-" + key + "=")), default)
                               for key, default in [("online-slots", "2"), ("evaluation-slots", "2"), ("executor", "local-process"),
                                                    ("max-model-concurrent", "8"), ("max-tenant-concurrent", "16")]})
    if configurations[0] != configurations[1] or configurations[0]["online-slots"] != "1" or configurations[0]["evaluation-slots"] != "1":
        raise RuntimeError("Lab scaler requires matching API/worker limits, backend and one slot per queue")
    existing = run(KUBE + ["-n", "rank-dev", "get", "secret/execution-capacity", "--ignore-not-found", "-o", "name"], capture=True).strip()
    if not existing:
        apply_json({"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "execution-capacity", "namespace": "rank-dev"},
                    "stringData": {"token": secrets.token_hex(32)}})
        run(KUBE + ["-n", "rank-dev", "rollout", "restart", "deployment/execution-api"])
    run(KUBE + ["-n", "rank-dev", "rollout", "status", "deployment/execution-api", "--timeout=60s"])
    run(KUBE + ["apply", "-f", str(HERE / "base/autoscaling.yaml")])
    run(KUBE + ["-n", "rank-dev", "wait", "--for=condition=Ready", "scaledobject/execution-worker", "--timeout=60s"])


def disable():
    run(KUBE + ["-n", "rank-dev", "delete", "scaledobject/execution-worker", "triggerauthentication/execution-capacity", "--ignore-not-found"])
    run(KUBE + ["-n", "rank-dev", "scale", "deployment/execution-worker", "--replicas=2"])
    run(KUBE + ["-n", "rank-dev", "rollout", "status", "deployment/execution-worker", "--timeout=60s"])


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["install", "enable", "disable", "status"])
    parser.add_argument("--registry-mirror", help="optional public GHCR mirror; official manifest digests remain fixed")
    args = parser.parse_args()
    if args.action == "install":
        install(args.registry_mirror)
    elif args.action == "enable":
        enable()
    elif args.action == "disable":
        disable()
    else:
        run(KUBE + ["-n", "rank-dev", "get", "scaledobject,hpa"])
        run(KUBE + ["-n", "rank-dev", "get", "deployment/execution-worker"])
