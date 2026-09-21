# Worker failure and replay verification — 2026-09-15

English | [中文](2026-09-15-worker-failure.zh.md)

Known worker failures retain their error, output, usage and result artifact through local and remote Execution. [Recorded evidence](2026-09-15-worker-failure.json) includes source hashes, exact images and eight passing complete-platform checks on minikube. The platform uses PostgreSQL/River, S3-compatible SeaweedFS, actual DSH processes and deterministic local model fixtures.

The packaged example requests a schema that rejects the deterministic Harness output. The public API returns `failed` with the original schema error, `{"status":"ok"}`, known usage and a readable failed-result artifact after cleanup. Resubmitting the same request returns the same ID and attempt 1. Both local and OpenSandbox snapshots pass with SQLite and with two PostgreSQL/River Workers. The cluster verifier repeats this scenario against S3, then verifies hotspot collection, multi-turn chat, trials, control conversation, SSE/artifact reads, Pod replacement and queue recovery.

The experiment exposed a PostgreSQL-specific replay failure: jsonb rewrites raw schema formatting, while the application compared bytes. The comparison uses JSON values, preserves array order and exact decimal values, and treats object key order and equivalent decimal notation as the same input. Focused tests distinguish adjacent integers above floating-point precision and reject changed arrays or types.

The pinned execd reports process exit 1 as a `CommandExecError` frame without an `execution_complete` frame. The adapter accepts that normal exit only with an identified, consistent failed worker response and a successfully read stream. Tests reject malformed events, contradictory termination, abnormal exits and truncated HTTP bodies, including interruption after an exit frame. Transport failure still reports an unknown outcome; this change does not authorize replay.

Focused Go tests, packaged snapshots, contract lint and whitespace checks pass. Documentation checks pass 27 of 28 checks; the unchanged root AGENTS word budget remains exceeded (2,398 versus 1,950). This experiment does not establish production identity, cross-host capacity, real provider behavior or RPM/TPM limits. Reproduce with the [lifecycle example](../../examples/execution-lifecycle/README.md) and [platform tutorial](../k8s/README.md).
