# Agent Note: Remote Execution through OpenSandbox

Status: implemented

English | [中文](2026-09-15-execution-opensandbox.zh.md)

## Problem

A worker process and its local child processes share a host. Separating River consumers does not independently bound a remote runtime's lifetime or preserve its identity after the consumer disappears. Sandbox creation and command execution can also succeed while their responses are lost.

## Decision

The OpenSandbox Executor runs a complete packaged DSH or deterministic Harness in a disposable container. Rank retains its Execution API; sandbox lifecycle remains internal to the Executor. Selection is explicit and never falls back to local execution. An incompatible consumer snoozes the frozen request without starting its runtime.

PostgreSQL schema 6 and SQLite schema 10 persist runtime creation intent, service endpoint, ownership scope, immutable image, absolute expiry and remote handle separately from the worker owner. Creation intent precedes the single create request; the handle precedes command access. SDK retries are disabled for creation and commands. Terminal cleanup reconciliation can discover a lost handle by exact scope, execution and attempt metadata, delete the owned runtime and confirm deletion. No matching resource after an ambiguous create remains unknown because a late create is still possible. Endpoint or scope changes cannot authorize cleanup through another service. Confirmation clears the existing cleanup obligation without changing the business result.

The image's protected root PID 1 starts execd with a restricted environment and exits at the persisted deadline. The kernel then terminates the PID namespace, including detached descendants. Harness commands run as UID/GID 1000, without the execd token, Docker socket, database credentials or lifecycle key. The authenticated command proxy must share the lifecycle service origin; redirects are rejected. CPU and memory are configured per runtime. The service-side TTL has a 60-second protocol minimum, while PID 1 enforces shorter execution deadlines independently.

The Linux runtime closure contains the architecture-specific Landlock helper at the entry package version, with archive integrity verification, and a pinned Bubblewrap package. DSH keeps its existing read-only shell policy and runtime capability probes. Installing a helper does not establish kernel support; failure to find a usable confinement provider rejects the tool instead of running it unconfined.

Private input upload uses execd's digit-encoded permission value `600`. Artifact export runs as the Harness user and opens paths within the artifact root, rather than invoking the privileged file-download endpoint on a Harness-selected path. Transfers enforce count, byte bounds and a final digest; known credentials are redacted, including compressed DSH logs, before content-addressed publication. Normal completion publishes artifacts before destroying the runtime. Upload or command failure still attempts bounded cleanup; unconfirmed shutdown retains capacity. [Observed evidence](2026-09-15-execution-failure-evidence.md) preserves known output, usage and confirmed references on failure.

Command completion and transport keepalives are independent: `ping` frames after a terminal event are ignored. Output or a second terminal event after completion, malformed frames, and truncated HTTP bodies still produce an unknown outcome. A keepalive alone never establishes completion.

[Resource pools](2026-09-15-execution-runtime-pool.md) freeze the allocation target and per-container limits before queuing and reserve a shared CPU/memory budget before runtime creation. Unknown attempts retain this budget through the same cleanup obligation.

The dedicated Docker lab enforces a [bounded writable-storage profile](2026-09-15-sandbox-storage.md) for its packaged image. This is deployment configuration, not a guarantee of arbitrary OpenSandbox services.

The [egress implementation](2026-09-15-sandbox-egress.md) freezes transport rules alongside the allocation plan. The dedicated Docker profile invokes protected network initialization before exposing execd and drops network-administration capabilities through PID 1 replacement.

Idempotency compares the persisted JSON representation of the request. Empty optional collections and JSON numbers survive repeat submissions without a false conflict; changed specifications still conflict.

## Alternatives considered

**Expose a second public Sandbox API.** It adds lifecycle choices to Rank callers without an accepted need to manage reusable environments. Execution remains the caller's unit of work.

**Trust service TTL or retry a lost command.** A dead service cannot run its TTL cleanup, and a repeated command can repeat model charges or tool effects. Independent runtime expiry and explicit unknown outcomes preserve those distinctions.

**Use privileged downloads for artifacts.** The Harness controls file paths and symlinks. A confined reader with the same unprivileged identity avoids letting those paths read the daemon's private files.

## Consequences

The [Docker lab](../../../../rank/deploy/k8s/README.md) runs the lifecycle service inside Linux so its proxy can reach Docker bridge addresses on macOS. The service alone receives the Docker socket; it is trusted infrastructure. The [remote example](../../../../rank/examples/execution-lifecycle/README.md) snapshots actual packaged API results in SQLite and PostgreSQL/River modes. Real DSH integration uses a deterministic model service. Fault tests exercise lost create responses, delayed discovery, failed handle persistence, foreign endpoints and stale attempts; the independent-lifetime experiment kills the lifecycle service while a detached task runs.

The optional minikube mode connects the external Docker service through a Kubernetes Service/EndpointSlice. API/Worker Pods use the remote adapter and S3 while Harness containers run outside the worker Pods. The full platform verifier checks remote allocation identity, endpoint/chat/trial behavior, retained S3 evidence and recovery after Rank replacement and worker scale-to-zero. Pod readiness is followed by gateway health before durable result reads. Backend changes are maintenance operations for an idle lab; accepted queued work retains its frozen backend.

The [Worker owner-loss experiment](../../../../rank/deploy/reports/2026-09-15-owner-loss.md) observes a real Bash tool effect and rejected filesystem write before confirming the Worker process exits from SIGKILL. Unknown outcomes, cancellation after the crash and committed-result replay match an assembled snapshot. The remote runtime is deleted before its cleanup reservation clears, and the external effect occurs once. This is deterministic-provider evidence on the recorded Linux kernel and security profile.

This adapter does not establish production egress policy, hard disk quotas, arbitrary hostile-code isolation, sidecar cleanup, workspace resume or physical-host capacity. Its server deployment must not inject host mounts or unsolicited sidecars. Empty discovery after an ambiguous create can retain reservations indefinitely. The [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md) retains the remaining isolation, deployment and scaling acceptance work; the [lab report](../../../../rank/deploy/reports/2026-09-15-opensandbox.md) records the tested scope.
