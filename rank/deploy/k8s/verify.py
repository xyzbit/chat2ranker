"""Exercise durable Execution, Pod replacement and bounded scaling in rank-dev only."""
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import argparse
import hashlib
import http.client
import json
from pathlib import Path
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

from lab import KUBE, ROOT, STATE, run


def wait(check, seconds=60):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(0.1)
    raise TimeoutError("Execution lab condition did not converge")


def kubectl(*args):
    return run(KUBE + ["-n", "rank-dev", *args], capture=True)


def pods():
    return json.loads(kubectl("get", "pods", "-l", "app=execution-worker", "-o", "json"))["items"]


def scale(count):
    kubectl("scale", "deployment/execution-worker", "--replicas=" + str(count))
    kubectl("rollout", "status", "deployment/execution-worker", "--timeout=60s")
    wait(lambda: len(pods()) == count)


def main(retained_report=None):
    secrets = json.loads((STATE / "execution-lab.json").read_text())
    report = {"images": json.loads((STATE / "images.json").read_text()), "checks": {}, "loads": []}
    report["startedAt"] = datetime.now(timezone.utc).isoformat()
    nodes = json.loads(run(KUBE + ["get", "nodes", "-o", "json"], capture=True))["items"]
    report["nodes"] = [{"name": node["metadata"]["name"], "runtime": node["status"]["nodeInfo"],
                        "allocatable": node["status"]["allocatable"]} for node in nodes]
    deployment = json.loads(kubectl("get", "deployment/execution-worker", "-o", "json"))
    worker = deployment["spec"]["template"]["spec"]["containers"][0]
    report["workerConfig"] = {key: worker[key] for key in ("image", "args", "resources")}
    prefix = "k8s-" + uuid.uuid4().hex
    forwarding = None
    changed_workers = False

    def connect():
        nonlocal forwarding
        if forwarding is not None:
            forwarding.terminate()
            forwarding.wait(timeout=10)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        args = KUBE + ["-n", "rank-dev", "port-forward", "--address=127.0.0.1", "service/execution-api", str(port) + ":8790"]
        forwarding = subprocess.Popen(args,
                                      stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        address = "http://127.0.0.1:" + str(port)

        def healthy():
            nonlocal forwarding
            if forwarding.poll() is not None:
                forwarding = subprocess.Popen(args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            try:
                with urllib.request.urlopen(address + "/v1/health", timeout=2) as response:
                    return response.status == 200
            except (urllib.error.URLError, OSError, http.client.HTTPException):
                return False
        wait(healthy)
        return address

    base = ""

    def request(path, body=None, headers=None, raw=False):
        auth = {"Authorization": "Bearer " + secrets["EXECUTION_SERVICE_TOKEN"], "Content-Type": "application/json"}
        auth.update(headers or {})
        req = urllib.request.Request(base + path, headers=auth,
                                     data=None if body is None else json.dumps(body).encode())
        with urllib.request.urlopen(req, timeout=30) as response:
            data = response.read().decode()
            return data if raw else json.loads(data)

    def submit(key, prompt="quick"):
        return request("/v1/executions", {"idempotencyKey": prefix + key, "spec": {
            "protocolVersion": 2, "kind": "agent", "harness": "dsh", "prompt": prompt,
            "metadata": {"responseId": prefix + key, "tenantId": prefix}}})

    def get(identifier):
        return request("/v1/executions/" + identifier)

    def terminal(identifier):
        def check():
            value = get(identifier)
            return value if value["status"] in ("completed", "failed", "cancelled") else None
        return wait(check, 90)

    def events(identifier, after=0):
        value = request("/v1/executions/" + identifier + "/events",
                        headers={"Last-Event-ID": str(after)}, raw=True)
        return [json.loads(line[6:]) for line in value.splitlines() if line.startswith("data: ")]

    def cancel(identifier):
        request("/v1/executions/" + identifier + "/cancel", {})

    try:
        kubectl("rollout", "status", "deployment/execution-api", "--timeout=60s")
        base = connect()
        if retained_report is not None:
            previous = json.loads(retained_report.read_text())
            assert previous["passed"] and previous["images"] == report["images"]
            identifier = previous["checks"]["podReplacementPersistence"]
            complete = get(identifier)
            assert complete["status"] == "completed" and complete["attempt"] == 1
            frames = events(identifier)
            assert len(frames) == previous["checks"]["queueIdempotencyReplay"]["events"]
            assert events(identifier, 2) == frames[2:]
            artifact = next(item for item in complete["result"]["artifacts"] if item["kind"] == "stdout")
            path = "/v1/executions/" + identifier + "/artifacts?path=" + urllib.parse.quote(artifact["path"], safe="")
            content = request(path)["content"]
            expected = json.loads((ROOT / "rank/examples/execution-lifecycle/snapshot.json").read_text())["artifact"]
            assert content == expected and hashlib.sha256(content.encode()).hexdigest() == artifact["sha256"]
            unknown = get(previous["checks"]["ownerLossWithoutReplay"]["executionId"])
            assert unknown["status"] == "failed" and unknown["attempt"] == 1
            report["checks"]["clusterRestartPersistence"] = identifier
            report["sourceReport"] = retained_report.name
            report["passed"] = True
            return
        print("Verifying accepted work without workers and queued cancellation", flush=True)
        changed_workers = True
        scale(0)
        queued = submit("queued")
        assert get(queued["id"])["status"] == "queued"
        cancel(queued["id"])
        result = terminal(queued["id"])
        assert result["status"] == "cancelled" and result["attempt"] == 0
        retained = submit("retained")
        scale(2)
        complete = terminal(retained["id"])
        assert complete["status"] == "completed", complete
        assert submit("retained")["id"] == retained["id"]
        frames = events(retained["id"])
        assert [frame["sequence"] for frame in frames] == list(range(1, len(frames) + 1))
        assert events(retained["id"], 2) == frames[2:]
        artifact = next(item for item in complete["result"]["artifacts"] if item["kind"] == "stdout")
        path = "/v1/executions/" + retained["id"] + "/artifacts?path=" + urllib.parse.quote(artifact["path"], safe="")
        content = request(path)["content"]
        assert hashlib.sha256(content.encode()).hexdigest() == artifact["sha256"]
        report["checks"]["queueIdempotencyReplay"] = {"executionId": retained["id"], "events": len(frames)}

        print("Verifying running cancellation", flush=True)
        active = submit("cancel", "hold")
        wait(lambda: get(active["id"])["status"] == "running")
        cancel(active["id"])
        assert terminal(active["id"])["status"] == "cancelled"
        report["checks"]["runningCancellation"] = active["id"]

        print("Replacing workers, API, PostgreSQL and object service; checking retained evidence", flush=True)
        old_workers = {pod["metadata"]["uid"] for pod in pods()}
        for deployment in ("execution-worker", "execution-api", "postgres", "objects"):
            kubectl("rollout", "restart", "deployment/" + deployment)
            kubectl("rollout", "status", "deployment/" + deployment, "--timeout=60s")
        wait(lambda: not old_workers.intersection(pod["metadata"]["uid"] for pod in pods()))
        base = connect()
        assert get(retained["id"])["status"] == "completed"
        assert request(path)["content"] == content
        assert events(retained["id"]) == frames
        report["checks"]["podReplacementPersistence"] = retained["id"]

        print("Measuring 24 bounded fixture requests with one and two workers", flush=True)
        for count in (1, 2):
            scale(count)
            start = time.monotonic()
            with ThreadPoolExecutor(max_workers=8) as pool:
                submitted = list(pool.map(lambda index: submit(f"load-{count}-{index}"), range(24)))
                results = list(pool.map(lambda item: terminal(item["id"]), submitted))
            assert all(item["status"] == "completed" and item["attempt"] == 1 for item in results), results
            owners = sorted({item["externalHandle"] for item in results})
            assert len(owners) == count, owners
            timing = lambda value: datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()
            intervals = sorted((timing(item[field]), delta) for item in results
                               for field, delta in (("startedAt", 1), ("completedAt", -1)))
            concurrent = peak = 0
            for _, delta in intervals:
                concurrent += delta
                peak = max(peak, concurrent)
            assert peak <= count, peak
            queued_seconds = sorted(timing(item["startedAt"]) - timing(item["createdAt"]) for item in results)
            report["loads"].append({"workers": count, "requests": len(results), "seconds": round(time.monotonic() - start, 3),
                                    "owners": owners, "peakConcurrent": peak, "queueP95Seconds": round(queued_seconds[22], 3)})

        print("Removing an active owner; verifying unknown outcome without replay", flush=True)
        active = submit("owner-loss", "hold")
        wait(lambda: get(active["id"])["status"] == "running")
        owner = get(active["id"])["externalHandle"]
        pod = next(item["metadata"]["name"] for item in pods() if owner.startswith(item["metadata"]["name"] + "-"))
        kubectl("delete", "pod", pod, "--grace-period=0", "--force", "--wait=false")
        outcome = terminal(active["id"])
        assert outcome["status"] == "failed" and outcome["attempt"] == 1, outcome
        assert "execution_outcome_unknown" in outcome["error"], outcome
        assert sum(item["type"] == "execution.running" for item in events(active["id"])) == 1
        report["checks"]["ownerLossWithoutReplay"] = {"executionId": active["id"], "status": outcome["status"], "error": outcome["error"]}

        print("Rolling workers with accepted requests", flush=True)
        pending = [submit("roll-" + str(index)) for index in range(8)]
        kubectl("rollout", "restart", "deployment/execution-worker")
        kubectl("rollout", "status", "deployment/execution-worker", "--timeout=60s")
        assert all(terminal(item["id"])["status"] == "completed" for item in pending)
        report["checks"]["rollingAcceptedWork"] = len(pending)
        report["passed"] = True
    finally:
        if forwarding is not None:
            forwarding.terminate()
            forwarding.wait(timeout=10)
        if changed_workers:
            scale(2)
        report["finishedAt"] = datetime.now(timezone.utc).isoformat()
        output = STATE / (prefix + "-report.json")
        output.write_text(json.dumps(report, indent=2) + "\n")
        print("Execution lab evidence:", output, flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--retained-report", type=Path, help="Only read and verify evidence from a passed full lab report")
    main(parser.parse_args().retained_report)
