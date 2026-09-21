# Execution autoscaling experiment — 2026-09-15

English | [中文](2026-09-15-autoscaling.zh.md)

The [machine-readable report](2026-09-15-autoscaling.json) records a successful bounded KEDA run and the preceding failed run. The [repeatable verifier](../k8s/verify-autoscaling.py) uses packaged Execution API/Worker Pods, PostgreSQL/River, real DSH processes in separate OpenSandbox containers, and a deterministic model with a ten-second response delay. All services run on one laptop. The minikube profile requests four CPUs and 4 GiB; Kubernetes reports eight CPUs and 8,025,700 KiB allocatable. Neither value establishes independent physical capacity. Remote sandbox containers are outside Kubernetes scheduling, so Worker scaling does not provision additional Docker hosts.

| Phase | Observed result |
| --- | --- |
| Load: 16 requests, model concurrency four | All 16 completed at attempt one; ready workers grew from one to three. |
| Drain | Desired and ready workers returned from three to one after eligible demand cleared. |
| Model concurrency one, eight requests | During the 40-second observation, demand stayed at or below one and workers stayed at one despite quota-blocked work. Two requests completed; six were cancelled afterward. |
| Cleanup and restoration | No submitted runtime retained pending cleanup. The temporary model was removed; API/Worker limits returned to four, the lab scaler/HPA was removed, and two manual workers were ready. |
| Proxy networking | Inspection of a running sandbox found no host-port bindings; remote execution completed through the authenticated server proxy. |

For the 16-request burst, creation-to-start queue wait was p50 **53.30 s**, p95/max **107.75 s**. These values include runtime provisioning, a deliberately delayed model and gradual scale-up from one worker. This experiment proves bounded scaling and task accounting; it does not meet or define an interactive-chat latency objective. A production decision needs arrival-rate tests, warm-capacity measurements and real model budgets.

The preceding run completed 14 requests and failed two when OpenSandbox 0.2.3 selected host ports already bound on Docker. Its allocator probed the lifecycle server's container namespace. The lab uses a [version-checked build patch](../k8s/opensandbox-proxy-only.py) to disable unnecessary host-port publication. Exact execution/attempt/scope labels on Docker create/destroy events supplied the two missing handles for an audited operator repair. The ordinary reconciler confirmed absence and released cleanup reservations; both failed outcomes remain failed, and neither invocation was replayed. The successful rerun uses the patched server image identified in the report.

A complete platform regression with the patched server also passes gateway authentication, daily-news requests, multi-turn OpenAI SDK chat, Agent trials, control-session persistence, S3/SSE reads, Rank/control Pod replacement and worker zero-to-two recovery. The queued response completes at attempt one after recovery.

This run covers online demand, an application concurrency quota, idle drain and finite cancellation. It does not establish provider RPM/TPM handling, evaluation-pool performance, interruption at every ownership transition, production authentication, disk/egress isolation, physical-host scaling or high availability. Setup is in [the lab tutorial](../k8s/README.md); metric semantics are in [the capacity decision](../../../.agents/notes/implemented/architecture/2026-09-15-execution-capacity.md).
