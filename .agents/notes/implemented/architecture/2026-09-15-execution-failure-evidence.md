# Agent Note: Preserve observed results across artifact failure

Status: implemented

English | [中文](2026-09-15-execution-failure-evidence.zh.md)

## Problem

A model can finish and produce known usage before all artifacts reach durable storage. Treating the result as available only after every upload discards the reply, usage and already published references when a later transfer fails. Replaying the invocation to recreate files can duplicate external operations.

## Decision

Both Executors retain valid worker failure responses through the same evidence publication path as successful responses. Protocol version, execution identity and terminal status must agree before publication. The remote command stream distinguishes a normally exited worker (exit 1 with a matching failed response) from interrupted transport, malformed output, abnormal termination or contradictory success. A known failure remains failed with its original error, output, usage and retrievable artifacts; artifact-publication errors are appended rather than replacing the worker error. Neither result causes automatic invocation replay.

Local and OpenSandbox Executors persist the sanitized observed result before artifact publication, with no unconfirmed artifact references. Each successful upload adds its content-addressed reference and persists the cumulative evidence before proceeding to the next artifact. SQLite uses the existing result column; PostgreSQL uses the existing JSON result field. No storage format migration is required.

`Repository.RecordEvidence` accepts only the matching nonzero attempt in running or cancelling state. A terminal record rejects late evidence. It updates the observed result without changing lifecycle status, acknowledging a River job or releasing runtime capacity. Failure, cancellation and owner-loss transitions preserve evidence already committed. Normal completion still atomically publishes the complete result and terminal event with River acknowledgement, after required artifacts and confirmed cleanup.

An Execution `result` can therefore exist while running, failed or cancelled. Its presence does not mean the invocation succeeded: consumers use `status`. Artifact reads authorize against only the recorded references, including those on failed executions. The record retains known output and usage even if no artifact succeeded. An upload committed remotely but not referenced because its database write failed remains an unreferenced object; the existing operator retention policy applies. Unuploaded files are not advertised as retrievable.

The adapters stop on evidence persistence or artifact transfer failure and attempt bounded cleanup. They never rerun model or tool work to restore artifacts. A database outage can prevent an observed result from becoming durable; recovery reports an unknown outcome when no authoritative terminal result was committed. In-memory output is not a recovery guarantee, and this decision does not retain an unbounded Sandbox for later manual extraction.

Artifact previews preserve bytes through JSON: `encoding` is `utf-8` for valid text and `base64` otherwise. `truncated` refers to the one-MiB byte prefix, before encoding. Decode base64 before digest verification; a truncated preview is not a complete-file checksum or download. Rank forwards the encoding and the UI offers a binary download only when the response is complete.

## Alternatives considered

**Publish all references only at terminal success.** Makes the success path smaller but loses known usage and successfully uploaded evidence on a later failure.

**Return success despite missing required artifacts.** Hides an incomplete execution and violates the requirement that referenced evidence be durable before completion.

**Rerun the Harness after storage recovers.** Can duplicate paid model calls and external tool effects; a queue retry does not authorize that replay.

## Consequences

SQLite and real PostgreSQL tests cover evidence retention, cancellation, mismatched attempts and terminal write rejection. The local publication test interrupts the second upload and checks that only the first file is authorized. The [dependency verifier](../../../../rank/deploy/k8s/verify-dependencies.py) runs real DSH tools, removes PostgreSQL after an observed effect, and interrupts selected S3 request bodies on actual TCP streams. Its snapshot requires one execution attempt, one external effect, confirmed cleanup, and known evidence only in the completed-model/upload-failure case. Existing artifacts and a fresh post-recovery invocation must remain readable.

The [packaged lifecycle examples](../../../../rank/examples/execution-lifecycle/README.md) intentionally reject a deterministic Harness output against its requested schema. Local and remote paths require a failed status, retained output/usage/result artifact, one attempt on repeated submission and confirmed cleanup, with SQLite and PostgreSQL/River. Command-stream tests reject truncated transport even after a process-exit frame.

Each artifact adds a database write of the cumulative result. Counts and sizes remain bounded by Executor configuration. This is a current-attempt evidence snapshot, not a general checkpoint engine or a promise to recover uncommitted memory. Remaining production requirements are owned by the [proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md).
