# Remote Worker owner-loss experiment — 2026-09-15

English | [中文](2026-09-15-owner-loss.zh.md)

The [recorded results](2026-09-15-owner-loss.json) identify the images and observed outcomes on one minikube node and a separate Docker Sandbox service on the same laptop. A deterministic model directs the real DSH Bash tool to attempt a forbidden write, then call a local HTTP service that records the effect before delaying its reply. The verifier confirms the owning container's identity and sends SIGKILL to its process, requiring exit code 137.

| Scenario | Observed result |
| --- | --- |
| Worker killed after tool effect | One effect, one rejected write, one attempt; terminal `execution_outcome_unknown`. Runtime deletion is confirmed and cleanup reservations clear. |
| Cancellation after the same crash | The same unknown outcome is retained; cancellation does not claim that the external effect was undone. |
| Worker killed after result commit | The completed result survives identical resubmission with one attempt and one running event. |

Both interrupted HTTP effects close, and neither is repeated. The [assembled snapshot](../k8s/fixtures/owner-loss.snapshot.json) records API outcomes and externally observed tool facts without IDs, timing or credentials. Full platform regression on these images passes endpoint calls, daily-news, structured chat, trials, control history, S3 reads, SSE replay and restart/queue recovery.

The ARM64 image includes the pinned Landlock helper and Bubblewrap 0.8.0-2+deb12u1. This Docker Desktop kernel, `6.10.14-linuxkit`, does not enforce Landlock; DSH's existing Bubblewrap provider executes the unchanged read-only policy. The host already uses an unconfined Docker seccomp profile; this experiment does not change it. Successful probes on this host do not establish support for other kernel, seccomp or user-namespace configurations. These checks do not establish arbitrary hostile-code isolation, production identities, disk/egress limits or physical-host capacity. Repeat commands and prerequisites are in [the lab tutorial](../k8s/README.md#worker-owner-loss-experiment).
