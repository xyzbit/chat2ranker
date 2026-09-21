"""Verify bounded KEDA growth, quota exclusion and drain with real remote DSH work."""
from datetime import datetime, timezone
import importlib.util
import json
import math
import socket
import subprocess
import time
import urllib.error
import urllib.request
import uuid

from lab import HERE, KUBE, STATE, run
from verify import kubectl, wait

module_spec = importlib.util.spec_from_file_location("autoscaling_lab", HERE / "autoscaling-lab.py")
scaling = importlib.util.module_from_spec(module_spec)
module_spec.loader.exec_module(scaling)


def main():
    mode = json.loads((STATE / "platform-mode.json").read_text())
    if mode["executor"] != "opensandbox":
        raise RuntimeError("This experiment requires platform-lab.py up --sandbox")
    if kubectl("get", "scaledobject/execution-worker", "--ignore-not-found", "-o", "name").strip():
        raise RuntimeError("Disable the existing lab scaler before this experiment")
    original = {name: json.loads(kubectl("get", "deployment/" + name, "-o", "json"))["spec"]["template"]["spec"]["containers"][0]["args"]
                for name in ["execution-api", "execution-worker"]}
    secret = json.loads((STATE / "execution-lab.json").read_text())["EXECUTION_SERVICE_TOKEN"]
    images = json.loads((STATE / "platform-images.json").read_text())
    prefix = "capacity-" + uuid.uuid4().hex
    container = "rank-" + prefix
    report = {"startedAt": datetime.now(timezone.utc).isoformat(), "images": images, "kedaVersion": scaling.KEDA_VERSION,
              "mode": mode["executor"], "samples": [], "executions": [], "checks": {}}
    path = STATE / (prefix + ".json")
    worker = json.loads(kubectl("get", "deployment/execution-worker", "-o", "json"))["spec"]["template"]["spec"]["containers"][0]
    report["workerConfig"] = {key: worker[key] for key in ["image", "args", "resources"]}
    report["nodeAllocatable"] = json.loads(run(KUBE + ["get", "node/rank-dev", "-o", "json"], capture=True))["status"]["allocatable"]
    forward = None
    base = ""
    submitted = []
    enabled = False
    changed_limits = False

    def connect():
        nonlocal forward, base
        if forward is not None:
            forward.terminate(); forward.wait(timeout=10)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0)); port = listener.getsockname()[1]
        forward = subprocess.Popen(KUBE + ["-n", "rank-dev", "port-forward", "--address=127.0.0.1", "service/execution-api", str(port) + ":8790"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        base = "http://127.0.0.1:" + str(port)
        def healthy():
            try:
                with urllib.request.urlopen(base + "/v1/health", timeout=2) as response:
                    return response.status == 200
            except (urllib.error.URLError, OSError):
                return False
        wait(healthy, 30)

    def request(path, body=None):
        req = urllib.request.Request(base + path, headers={"Authorization": "Bearer " + secret, "Content-Type": "application/json"},
                                     data=None if body is None else json.dumps(body).encode())
        with urllib.request.urlopen(req, timeout=30) as response:
            return json.load(response)

    def sample(phase):
        capacity = request("/v1/capacity")
        deployment = json.loads(kubectl("get", "deployment/execution-worker", "-o", "json"))
        value = {"phase": phase, "at": datetime.now(timezone.utc).isoformat(), "desired": deployment["spec"]["replicas"],
                 "ready": deployment["status"].get("readyReplicas", 0), "capacity": capacity}
        if not report["samples"] or any(report["samples"][-1][key] != value[key] for key in ["phase", "desired", "ready"]):
            print("Capacity phase:", phase, "desired:", value["desired"], "ready:", value["ready"], flush=True)
        report["samples"].append(value)
        if not 1 <= value["desired"] <= 3:
            raise AssertionError("Scaler exceeded the 1..3 worker range")
        return value

    def idle(phase):
        time.sleep(1)
        value = sample(phase)
        return value["desired"] == value["ready"] == 1 and value["capacity"]["online"]["demand"] == 0

    def submit_batch(connection, count):
        result = []
        for number in range(count):
            key = prefix + "-" + str(len(submitted))
            value = request("/v1/executions", {"idempotencyKey": key, "spec": {"protocolVersion": 2, "kind": "agent", "harness": "dsh",
                "prompt": "Return a short acknowledgement.", "modelConnectionId": connection, "model": "rank-lab",
                "metadata": {"responseId": key, "tenantId": prefix}, "timeoutMs": 60000}})
            result.append(value["id"]); submitted.append(value["id"])
        return result

    def results(identifiers):
        return [request("/v1/executions/" + identifier) for identifier in identifiers]

    def finished(values):
        return all(value["status"] in ["completed", "failed", "cancelled"] and not value.get("runtimeCleanupPending") for value in values)

    def retain(values):
        for value in values:
            report["executions"].append({key: value.get(key) for key in ["id", "status", "attempt", "createdAt", "startedAt", "completedAt", "runtimeCleanupPending"]})

    def replace_limits(limit):
        for name, args in original.items():
            next_args = ["-max-model-concurrent=" + str(limit) if arg.startswith("-max-model-concurrent=") else arg for arg in args]
            kubectl("patch", "deployment/" + name, "--type=json", "-p", json.dumps([{"op": "replace", "path": "/spec/template/spec/containers/0/args", "value": next_args}]))
            kubectl("rollout", "status", "deployment/" + name, "--timeout=60s")
        connect()

    try:
        run(["docker", "run", "-d", "--name", container, "--label", "rank.lab=" + prefix, "--network", "rank-dev",
             "--cpus=0.5", "--memory=128m", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges",
             "--user=1000:1000", "-e", "RANK_LAB_DELAY_MS=10000", "--entrypoint", "node", images["images"]["platform"], "/opt/rank/fixtures/model.mjs"], capture=True)
        model_ip = run(["docker", "inspect", container, "--format", '{{(index .NetworkSettings.Networks "rank-dev").IPAddress}}'], capture=True).strip()
        enabled = True; scaling.enable()
        connect()
        capacity = request("/v1/capacity")
        if any(capacity[q][key] for q in ["online", "evaluation"] for key in ["active", "ready", "resourceBlocked", "quotaBlocked", "capabilityBlocked", "deliveryBlocked", "cleanupPending"]):
            raise RuntimeError("Run autoscaling verification on an idle lab")
        connection = request("/v1/model-connections", {"name": prefix, "provider": "custom", "protocol": "openai-chat-completions",
                             "baseUrl": "http://" + model_ip + ":8800/v1", "apiKey": "rank-lab-fixture", "defaultModel": "rank-lab"})
        request("/v1/model-connections/" + connection["id"] + "/verify", {})
        wait(lambda: idle("initial"), 120)
        identifiers = submit_batch(connection["id"], 16)
        deadline = time.monotonic() + 180
        while True:
            sample("load")
            values = results(identifiers)
            if not report["checks"].get("privateSandboxPorts"):
                for value in values:
                    handle = value.get("runtime", {}).get("handle")
                    if value["status"] != "running" or not handle:
                        continue
                    inspected = subprocess.run(["docker", "inspect", "sandbox-" + handle, "--format", "{{json .HostConfig.PortBindings}}"], capture_output=True, text=True)
                    if inspected.returncode:
                        if "No such object" in inspected.stderr:
                            continue
                        raise RuntimeError("Cannot inspect the owned runtime's ports")
                    if json.loads(inspected.stdout):
                        raise AssertionError("Remote runtime unexpectedly publishes host ports")
                    report["checks"]["privateSandboxPorts"] = True
                    break
            if any(value["status"] == "failed" for value in values):
                retain(values)
                raise AssertionError("A remote execution failed; inspect its retained outcome")
            if finished(values):
                break
            if time.monotonic() > deadline:
                raise TimeoutError("Load did not finish")
            time.sleep(1)
        retain(values)
        if not all(v["status"] == "completed" and v["attempt"] == 1 and v.get("runtime", {}).get("handle") for v in values):
            raise AssertionError("Load did not complete in one remote attempt")
        if max(s["ready"] for s in report["samples"] if s["phase"] == "load") != 3:
            raise AssertionError("Load never reached three ready workers")
        wait(lambda: idle("drain"), 150)
        if not report["checks"].get("privateSandboxPorts"):
            raise AssertionError("No running sandbox was inspected")
        report["checks"]["growthAndDrain"] = True
        changed_limits = True
        replace_limits(1)
        identifiers = submit_batch(connection["id"], 8)
        until = time.monotonic() + 40
        while time.monotonic() < until:
            value = sample("quota")
            if value["desired"] != 1 or value["capacity"]["online"]["demand"] > 1:
                raise AssertionError("Model quota caused unnecessary scaling")
            time.sleep(1)
        if not any(s["capacity"]["online"]["quotaBlocked"] > 0 for s in report["samples"] if s["phase"] == "quota"):
            raise AssertionError("Quota phase did not create blocked work")
        for value in results(identifiers):
            if value["status"] in ["queued", "running", "cancelling"]:
                request("/v1/executions/" + value["id"] + "/cancel", {})
        wait(lambda: finished(results(identifiers)), 90)
        retain(results(identifiers))
        report["checks"]["quotaExclusionAndCancellation"] = True
        report["checks"]["runtimeCleanup"] = all(not v.get("runtimeCleanupPending") for v in report["executions"])
        latencies = sorted((datetime.fromisoformat(v["startedAt"].replace("Z", "+00:00")) - datetime.fromisoformat(v["createdAt"].replace("Z", "+00:00"))).total_seconds()
                           for v in report["executions"][:16])
        report["loadQueueWaitSeconds"] = {"p50": latencies[math.ceil(len(latencies)*.5)-1], "p95": latencies[math.ceil(len(latencies)*.95)-1], "max": max(latencies)}
        report["kedaImages"] = [status["imageID"] for pod in json.loads(run(KUBE + ["-n", "rank-keda", "get", "pods", "-o", "json"], capture=True))["items"] for status in pod["status"].get("containerStatuses", [])]
    except Exception as error:
        report["failure"] = type(error).__name__
        raise
    finally:
        cleanup_errors = []
        def cleanup(name, operation):
            try:
                operation()
            except Exception as error:
                cleanup_errors.append({"operation": name, "error": type(error).__name__})
        def stop_work():
            if not submitted:
                return
            connect()
            for identifier in submitted:
                value = request("/v1/executions/" + identifier)
                if value["status"] in ["queued", "running", "cancelling"]:
                    request("/v1/executions/" + identifier + "/cancel", {})
            wait(lambda: finished(results(submitted)), 90)
        cleanup("stop submitted executions", stop_work)
        if enabled:
            cleanup("disable lab scaler", scaling.disable)
        if changed_limits:
            def restore(name, args):
                kubectl("patch", "deployment/" + name, "--type=json", "-p", json.dumps([{"op": "replace", "path": "/spec/template/spec/containers/0/args", "value": args}]))
                kubectl("rollout", "status", "deployment/" + name, "--timeout=60s")
            for name, args in original.items():
                cleanup("restore " + name, lambda name=name, args=args: restore(name, args))
        if forward is not None:
            forward.terminate()
            cleanup("stop port forward", lambda: forward.wait(timeout=10))
        cleanup("remove owned model fixture", lambda: run(["docker", "rm", "-f", container], capture=True))
        report["submittedExecutionIds"] = submitted
        report["cleanupErrors"] = cleanup_errors
        report["endedAt"] = datetime.now(timezone.utc).isoformat()
        path.write_text(json.dumps(report, indent=2) + "\n")
        print("Autoscaling evidence:", path)
        if cleanup_errors:
            raise RuntimeError("Lab cleanup needs attention; see the recorded operations")



if __name__ == "__main__":
    main()
