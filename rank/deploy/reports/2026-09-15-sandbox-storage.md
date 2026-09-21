# Bounded Sandbox storage — 2026-09-15

English | [中文](2026-09-15-sandbox-storage.zh.md)

The dedicated Docker runtime passes native-process storage exhaustion and the seven complete-platform checks on minikube. [Machine-readable evidence](2026-09-15-sandbox-storage.json) records the exact runtime/server images, source hashes and protected-lifetime result. Tests use actual DSH processes with a deterministic local model; they make no external model calls.

| Check | Observed result |
| --- | --- |
| Input, workspace and artifact files share 128 MiB | Aggregate writes across all three directories reach `ENOSPC`; freeing the files permits new writes. |
| Temporary and shared-memory storage | `/tmp` reaches its 32 MiB cap and `/dev/shm` its 16 MiB cap; both accept writes after cleanup. |
| File count | The workspace reaches its 8,192-inode allowance with empty files and returns `ENOSPC`; cleanup restores writes. |
| Alternative locations and mounts | UID 1000 writes to image paths fail. Additional request volumes, isolation overlays and image-declared volumes are rejected. Docker reports a read-only rootfs with no bind or volume mounts. |
| Memory and logs | Docker inspection confirms 512 MiB memory with no additional swap and local log rotation at 2 MiB across two files. Log flood behavior is not separately measured. |
| Normal platform behavior | Hotspot collection, multi-turn chat, trials, control conversation, S3/SSE, Pod replacement and queue recovery all pass. |
| Independent expiry | Killing the lifecycle server leaves the detached workload alive until protected PID 1 reaches its deadline, then terminates the container. The lifecycle service is restarted and the sandbox is deleted. |

The first packaged-platform run exposed a missing Node startup argument for the current DSH CLI configuration watcher. The adapter supplies `--expose-internals`; the passing report uses the corrected runtime image. The Go Harness tests include real structured-history serialization to a local provider.

These are ephemeral writable-memory limits, not persistent disk quotas or an exact host storage budget including container metadata. tmpfs usage consumes container memory; a memory-intensive workload can be stopped before reaching its file allowance. Docker configuration and process behavior were measured on this laptop. Production egress control, identities, independent-host capacity and hostile-kernel isolation are not established by this experiment. Reproduce using the [storage tutorial](../k8s/README.md#bounded-writable-storage).
