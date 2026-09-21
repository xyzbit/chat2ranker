# Execution lifecycle example

English | [中文](README.zh.md)

This keyless example builds and starts the shipping Execution binaries and invokes a deterministic CLI through the real process adapter. It checks submission, idempotent replay, ordered SSE, reconnecting from a sequence, artifact reads, queued cancellation, running cancellation and runtime cleanup reservations against [one snapshot](snapshot.json). It is an execution-protocol example, not a model-quality demonstration.

## Run locally

Use Go 1.26.0 and Node 24 or later. From the repository root:

```sh
node rank/examples/execution-lifecycle/verify.mjs
```

The verifier owns a temporary SQLite database, private service token, binaries and workspace. It stops its processes and removes those local files on exit. The waiting cancellation scenario uses a disabled evaluation queue; actual invocations use the online queue. The fixture spawns a child process so the packaged process test can independently verify descendant termination.

## Run with PostgreSQL and S3

Provide `EXECUTION_TEST_DATABASE_URL` through a private shell environment. The database role must be permitted to create/drop schemas. The same command starts a separate API and two River workers and removes only its randomly named `snapshot_*` schema afterward. Do not put credentials in command-line arguments or committed files.

To exercise remote artifact storage, also provide `EXECUTION_TEST_S3_ENDPOINT` as `host:port`, `EXECUTION_TEST_S3_BUCKET`, `EXECUTION_TEST_S3_ACCESS_KEY` and `EXECUTION_TEST_S3_SECRET_KEY`. The bucket must already exist. This local verifier explicitly uses HTTP for S3. It uploads artifacts, removes their local staging directory, restarts the API and verifies the same artifact bytes through the authenticated API. It leaves the uploaded objects in the disposable test bucket for inspection; bucket retention is owned by the lab operator.

The [Execution README](../../../execution/backend/README.md) defines service configuration and lifecycle limitations. The [production design](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md) defines the separate minikube and production acceptance work. Passing this example does not verify Rank serving, model quality or whole-runtime sandbox isolation.

## Run with OpenSandbox

Use [the sandbox lab](../../deploy/k8s/README.md) to build the protected runtime image and start the lifecycle service. Provide `EXECUTION_TEST_SANDBOX_URL` (including `/v1`), `EXECUTION_TEST_SANDBOX_API_KEY` and the immutable `EXECUTION_TEST_SANDBOX_IMAGE` through a private environment, then run:

```sh
node rank/examples/execution-lifecycle/verify-remote.mjs
```

The [remote snapshot](remote.snapshot.json) covers actual packaged Execution binaries, a remote deterministic Harness, frozen CPU/memory plans, persisted runtime ownership, events, repeated requests with empty metadata, artifact reads after sandbox deletion and queued cancellation without allocation. With `EXECUTION_TEST_DATABASE_URL`, the same verifier starts an API and two River workers and removes its own random `remote_*` schema afterward. This example uses local artifact storage shared by processes on one host; it does not prove cross-host artifact access. The lab additionally runs real DSH structured-history tests and a separate lifecycle-service-loss experiment.

The capacity snapshot checks authenticated aggregate counts with a disabled evaluation queue. It verifies that waiting work excluded from this pool does not advertise runnable demand.

The queue-expiry scenario submits to a disabled queue, restarts the API with a longer default, and verifies the original deadline, attempt-zero failure and two durable lifecycle events. It uses the same snapshot in SQLite and PostgreSQL/River.

Both variants also submit a deterministic structured-output failure. The same request retains its failed status and single attempt on replay, including after PostgreSQL JSON normalization. The recorded output, known usage and failure result artifact remain readable after cleanup. This validates worker failure handling without making a model-provider request.
