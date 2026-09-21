# Active Worker drain — 2026-09-15

English | [中文](2026-09-15-active-drain.zh.md)

The complete remote-runtime minikube platform passes all three active-drain scenarios with the existing Worker shutdown implementation. [Machine-readable evidence](2026-09-15-active-drain.json) records image identities, configuration, observed outcomes and source hashes. Nine accepted executions finish at attempt one: eight complete and one reports an interrupted unknown outcome. Each of the six real Bash tool calls produces exactly one external effect.

| Scenario | Observed outcome |
| --- | --- |
| Scale two Workers to one; release departing owner's tool during grace | The invocation completes with readable S3 evidence. The watched Worker exits with code zero after 8.992 seconds. A following online request stays queued until that owner exits, then completes on the surviving Worker. |
| Scale two Workers to one; keep the tool blocked | The watched Worker exits with code zero after 15.495 seconds, after the 15-second drain grace. The invocation fails explicitly with an unknown outcome, keeps attempt one and is not replayed. Its Sandbox is deleted before retained capacity is released. The survivor's request completes normally. |
| Rolling update with both original owners active | Both invocations complete; their old Workers exit with code zero after 7.671 and 7.828 seconds. A following request executes on a new Worker. Both original Sandboxes are deleted and their S3 output digests match. |

Timings measure local observation of Pod deletion and terminal container status, including Kubernetes reporting delay. The watcher retains exact Pod UIDs; object disappearance alone cannot pass. Every invocation has one running event and one terminal event. Repeating each original request returns the original ID/status/attempt. The snapshot checks these outcomes through real APIs and DSH processes against a deterministic local provider.

The verifier restores two Workers and prior rollout annotations, settles its requests, closes watch/forward processes and removes the model fixture. The passed run has no cleanup errors. The lab configuration uses one online slot per Worker, 60-second invocations, 15-second drain grace, two-second runtime cleanup, three-second persistence and 25-second Pod termination grace.

These are manual scale-down and rolling-replacement results on one laptop, not automatic KEDA downscale selection, physical-node failure, provider capacity or a zero-interruption guarantee for arbitrary long calls. An exit code of zero means the Worker stopped cleanly; the API's durable invocation status remains authoritative for business success. Reproduce using the [active-drain tutorial](../k8s/README.md#active-worker-drain-experiment).
