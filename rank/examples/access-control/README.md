# Access-control acceptance

English | [中文](README.zh.md)

From the repository root, run `pnpm rank:test:access` with Go 1.26.0 and Node available. The example builds Rank, Execution and its worker, creates temporary SQLite databases and high-entropy operator credentials, and starts loopback listeners with management authentication enabled. It uses the deterministic Harness and makes no external provider requests.

The example publishes endpoints for two tenants, calls the public Responses API and checks that tenant, endpoint and operation permissions also cover event streams, cancellation, approval and conversation continuation. It verifies that public keys cannot administer Rank, operator keys cannot act as public keys, publication records use the authenticated operator, and replacement public keys retain authorized history after the old key is revoked.

The same running Rank process observes operator-token rotation and rejects a broken credential-file replacement. Separate invocations refuse an unprotected non-loopback listener and authenticated configurations with missing or built-in development service credentials. Results must match [the snapshot](snapshot.json). Processes and temporary credentials are removed on exit; the command prints no tokens.

[Management configuration](../../backend/README.md#management-access) defines deployment credentials, TLS origins and service permissions. The [minikube verifier](../../deploy/k8s/README.md#complete-platform-experiment) repeats public checks against PostgreSQL/River and tests projected-file revocation through the gateway. This example verifies a shared operator workspace; it does not establish team-isolated Agent or Dataset ownership.

The runnable snapshot also submits browser login JSON, verifies cookie-based workspace reads and writes, rejects that cookie on the public API, and proves logout and credential rotation invalidate browser sessions. HTTP tests cover cross-origin requests, malformed login input, cookie flags and session replacement; session-store tests cover expiry and capacity.
