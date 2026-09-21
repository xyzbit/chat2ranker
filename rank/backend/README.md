# Rank backend

English | [中文](README.zh.md)

[Invocation feedback](../docs/response-feedback.md) supports request search, diagnostic details and reviewed capture into immutable dataset versions. Source references remain with cases; appends preserve earlier Run snapshots.

This Go module produces one binary:

- `rankd`: Rank API, version freezing, case scheduling, judging policy, result aggregation, and business persistence.

The backend depends on Rank Repository interfaces and the versioned Execution Service client. It does not import DSH implementation packages, start harness processes, or call `ctx.agents.create()`.

`rankd` freezes DatasetVersion, AgentVersion, and EvaluatorVersion, then expands a Run into Case × Trial records. Each Run always owns one Agent version. A comparison request persists one RunGroup and atomically creates one child Run per selected Agent version before dispatching them independently. Each Trial submits a candidate invocation, runs required deterministic checks first, and submits independent Rubric Judge invocations only when needed. The default is five Trials per case. Rank keeps quality, infrastructure, and grading failures separate; aggregates valid-Trial pass rate, reliable cases, pass^3, and split candidate/evaluation cost; and stores only Execution and Artifact references.

Browser clients follow `GET /api/runs/{id}/events` instead of polling worker processes. The shared [SQL store](internal/sqlstore/store.go) supports SQLite for local development and PostgreSQL for deployment, with separate explicit migrations and portable business queries.

Deployment publication uses durable [release tasks](../docs/release-policy.md) with optional regression Runs and transactional alias promotion. SQLite migrations 18–19 store rules, decisions, Agent-family dataset defaults, and recoverable tasks independently of immutable Agent versions and execution limits.

New deployments remain unpublished until an explicit release. Agent-version creation does not change their target. Public callers can use the bare deployment name; [serving references](../docs/serving-api.md#deployment-reference) define publication resolution and explicit previews.

Endpoints accept additional instructions and caller functions without policy switches. Instructions append to the frozen system prompt for one request; continuation does not inherit them. Caller functions retain schema validation and explicit caller execution. API key creation returns plaintext once and stores only its hash; lists cannot recover the key.

Response cancellation records durable intent before contacting Execution; the [serving API](../docs/serving-api.md#cancellation) defines pending and terminal responses.

[Conversation configuration](../docs/conversation-configuration.md) records typed form actions without secrets. Dataset changes create immutable versions; endpoint creation accepts explicit execution limits, and publication-threshold changes share the API transaction and validation rules.

Deployment lookup by Agent version searches immutable release history within the requested tenant, including versions replaced by later publications. Results order deployments by their newest matching release.

[Agent playground](../docs/agent-playground.md) reuses durable Responses for direct version previews and deployed conversations. SQLite migration 20 retains existing records while allowing operator-owned Responses without serving credentials and unpublished previews without a Deployment.

`rankd` requires Go 1.26.0 and forwards `EXECUTION_SERVICE_TOKEN` to the configured Execution API, including SSE requests. Rank and Execution own separate database schemas. PostgreSQL persistence does not make Rank coordinators safe to replicate. [Serving dispatch](../../.agents/notes/implemented/architecture/2026-09-14-rank-serving-dispatch.md) recovers public Responses and Agent trials; [evaluation dispatch](../../.agents/notes/implemented/architecture/2026-09-14-rank-evaluation-dispatch.md) recovers candidate/Judge invocations.

Connection-backed Agent creation and version editing freeze the selected model, using the verified connection default when omitted. Form and Rank conversation commands share resolution and protocol validation. Existing versions retain their model when connection defaults change; unresolved stored versions require a new explicit version before evaluation or serving. See [frozen model selection](../../.agents/notes/implemented/architecture/2026-09-14-rank-frozen-model-selection.md).

Serving and trial continuations freeze native message roles and call/result IDs for a supporting Harness; unsupported adapters reject before creating a Response. See the [structured-history decision](../../.agents/notes/implemented/architecture/2026-09-14-rank-structured-history.md).

## Management access

Set `-access-file` or `RANK_ACCESS_FILE` to an operator credential file. Non-loopback listeners refuse startup without it; loopback-only development may omit it. When the file is configured, set explicit `RANK_ACTION_SECRET`, `RANK_CONTROL_TOKEN` and `EXECUTION_SERVICE_TOKEN` values of at least 24 characters in every storage mode; built-in development secrets are rejected. The file is JSON with `version: 1` and an `operators` array. Each entry has a stable `subject` (ASCII letters, digits, `@._-`, up to 128 characters), `tokenSha256` (lowercase hex SHA-256 of an independently generated random token), and optional RFC 3339 `expiresAt`. Generate at least 32 random bytes per token; SHA-256 here is for high-entropy tokens, not human passwords. Keep plaintext tokens outside the repository and model conversation. The file contains only digests and accepts multiple active tokens for one subject during rotation.

Browsers sign in through the Rank page using the subject as account and its generated token as password. Non-browser clients may use `Authorization: Bearer <token>` or Basic authentication; unauthorized requests return JSON without a browser authentication challenge. `GET /api/identity` returns the authenticated subject and role. Replace the file atomically; every new authorization reads it, and missing/invalid files return 503 rather than retaining old permissions. An empty operator list revokes all operators. Kubernetes projected files update asynchronously. Revocation affects subsequent requests, not already accepted tasks or open streams.

Deploy behind a TLS gateway and forward the original Authorization header. Set `RANK_MANAGEMENT_ORIGIN` or `-management-origin` to the public origin, such as `https://rank.example.com`, when the gateway changes the scheme or host. Set the same origin on control-host. Unsafe requests with a different browser Origin are rejected. Health probes remain unauthenticated; gateway policy may further restrict them. CORS alone does not grant management access.

Operator access administers the shared installation, including serving tenants. Public API keys retain tenant/endpoint/operation permissions and cannot enter management routes. The separate control token grants only the explicit control-service route set; connection writes from that service reject model secrets. Direct publication and release-task creation use the operator subject. See [the access decision](../../.agents/notes/implemented/architecture/2026-09-15-rank-management-access.md) and [packaged acceptance](../examples/access-control/README.md). Enterprise login and team-isolated Agent/Dataset ownership are separate work.

`POST /api/auth/login` accepts same-origin JSON `{ "username": "<subject>", "password": "<token>" }` and issues an opaque HttpOnly, SameSite=Strict cookie scoped to `/`. The cookie is Secure with TLS or an HTTPS `-management-origin`. `POST /api/auth/logout` invalidates the server session and clears the cookie. Login, logout and cookie-authenticated mutations require an exact Origin match. API identity and login responses are not cached; public Responses routes never accept this cookie.

Browser sessions expire after `-session-ttl` (default `8h`, minimum `1s`), with at most `-session-capacity` active sessions per installation (default `1024`, minimum `1`). Rank stores cookie and credential digests in its database and revalidates the live operator file on every request. Fixed expiry, rotation and logout apply across API processes and restarts. Login removes expired rows; capacity exhaustion returns 503 without evicting other browsers. The database never stores the raw cookie or operator credential. The in-memory provider is available to isolated HTTP fixtures. Short human passwords, enrollment and password recovery require a dedicated identity provider.

## PostgreSQL configuration

Set `RANK_DATABASE_URL` privately and choose `-postgres-schema` (default `rank`, or `RANK_POSTGRES_SCHEMA`) and `-database-connections` (default 8). Run `rankd -migrate` with the migration role before starting the server. Startup checks the schema version and refuses an absent or unsupported version. PostgreSQL schema v4 initializes fresh databases or explicitly upgrades v1/v2/v3; it does not import SQLite data. Without `RANK_DATABASE_URL`, `-db` selects SQLite and migrations run on open.

`-database-timeout` defaults to `5s` and bounds PostgreSQL pool acquisition, query I/O and application transactions. Earlier caller deadlines win; synchronous serving and SSE retain their own lifetimes. Raw dependency deadlines return HTTP 503 `dependency_timeout`; clients must preserve request identity because a timeout does not prove a write rolled back. For longer explicit migrations, run `-migrate -database-timeout=45s`. See [database deadline semantics](../../.agents/notes/implemented/architecture/2026-09-15-postgres-deadlines.md).

PostgreSQL server mode requires explicit `RANK_ACTION_SECRET`, `RANK_CONTROL_TOKEN` and `EXECUTION_SERVICE_TOKEN`, each at least 24 characters, plus `-webhook-secret-dir` on persistent storage. These internal secrets do not replace operator authentication in Rank and control-host. Model credentials remain in Execution. The health response identifies the selected storage driver without exposing database settings.

Rank retains one dispatcher owner for recovery and model observers. PostgreSQL serializes short business write transactions with a schema-specific advisory transaction lock; calls to models and Execution run outside it. HTTP API replicas share this database and accept durable work without starting observers. See [API rolling updates](../../.agents/notes/implemented/bug-fix/2026-09-17-rank-api-rolling-drain.md) and the [storage decision](../../.agents/notes/implemented/architecture/2026-09-14-rank-postgres-storage.md).

## Process modes and HTTP draining

`-mode=all` runs the HTTP API and dispatcher together for development. PostgreSQL deployments use `-mode=api` for independently replaceable HTTP instances and exactly one `-mode=dispatcher` for serving/evaluation observers, release tasks and webhook delivery. The dispatcher exposes only `/api/health`; it discovers durable admissions at `-dispatch-retry-interval` without resetting work already being observed. Its replacement remains a single-owner operation. Database migrations and control-host replacement have separate maintenance requirements.

`-shutdown-timeout` defaults to `30s` and must be positive. SIGTERM closes the HTTP listener and waits for accepted requests; expiry closes remaining connections without cancelling durable Execution tasks. Clients must treat a stream without a terminal event as incomplete and retrieve/replay the existing Response ID. `-event-poll-interval` (default `1s`, positive) bounds shared response-event polling; local notifications may wake readers sooner. It does not change model execution timeouts. Kubernetes Pod grace must exceed preStop, HTTP drain and cleanup time; the lab uses 5s preStop, 30s drain and 45s Pod grace.

## Dispatch recovery

Accepted Responses and evaluation invocations retain their exact outbound Execution requests. Observation errors retry after `-dispatch-retry-interval` (default 1s); `-execution-observe-timeout` (default 15s) bounds each event observation so pending cancellation is revisited. Both values must be positive. Shutdown waits for local serving and evaluation observers without cancelling remote executions. Active legacy Responses lacking frozen requests prevent startup; complete or explicitly resolve them with their owning version before upgrading. Active remote Runs without durable dispatch format are also refused. Completed legacy history stays readable.

Evaluation admission freezes system Judge connection/model, tenant routing and timeout before candidate dispatch. Candidate requests share the Run/Trial admission transaction. Transport recovery reuses stored requests; failed candidates and Judges are not automatically rerun. Run cancellation exposes `cancelRequested: true` while remote outcomes are being resolved; `cancelled` is terminal. Untouched candidate intents are cancelled without launching workers. The UI displays “Stopping…” while cancellation is pending.

Artifact preview responses preserve the Execution `encoding` (`utf-8` or `base64`) and byte-prefix `truncated` flag. Consumers decode base64 for binary bytes and must not treat truncated content as a complete file.

## Verification

`go test ./...` verifies SQLite behavior. With a private `RANK_TEST_DATABASE_URL`, the same application/HTTP tests run against isolated PostgreSQL schemas, including reopening stored history; PostgreSQL-specific tests cover competing connection pools, rollback, migration version refusal and event order. Tests retain SQLite-specific migration checks on SQLite. Only test-owned schemas are removed.

From the repository root, `node --test --test-name-pattern='Serving executes Responses' apps/rank-web/tests/stage5-e2e.test.mjs` runs the packaged Rank/Execution/worker serving and release snapshots. Setting `RANK_TEST_DATABASE_URL` selects PostgreSQL for Rank, explicitly migrates a random schema and verifies history/outbox retrieval after restarting Rank. Execution uses SQLite in this scenario; the separate [Execution lifecycle example](../examples/execution-lifecycle/README.md) verifies River workers. The serving recovery snapshot also kills Rank before submission and after Execution acceptance with its HTTP reply discarded, verifying original identity and one terminal event. The evaluation snapshot additionally discards candidate/Judge submission replies and kills Rank, checking original execution identity and single result/cost projection. These tests do not establish the complete Kubernetes platform.
