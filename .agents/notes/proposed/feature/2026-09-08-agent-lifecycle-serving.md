# Agent Note: Agent lifecycle platform and Responses-compatible serving

Status: proposed

English | [中文](2026-09-08-agent-lifecycle-serving.zh.md)

## Problem

AgentKiln can define immutable Agent versions, run them in isolated workers, evaluate repeated trials, retain native artifacts, and attribute usage and cost. Its product API stops at experiments and evaluation runs, so an evaluated Agent cannot become a stable endpoint for an external application without a separate deployment system.

Exposing the internal Execution Service would grant callers control over harness, model connection, system prompt, tools, Skills, workspace-related metadata, and artifact paths. Its single `prompt` to `output` request also does not define tenant authorization, release aliases, conversation continuity, public stream events, request retention, or compatibility with an established client protocol.

The product therefore needs a serving control plane and public data plane that reuse immutable Agent versions and isolated execution while preserving the separation between an external invocation, an Execution attempt, and an evaluation Run.

## Proposal

Position AgentKiln as an Agent lifecycle platform with five connected capabilities: prepare, evaluate, release, serve, and observe. The current experiment product remains the authoring and evaluation application. A new serving domain owns deployments, release aliases, public Responses, conversations, tenant policy, and production audit records.

The public data plane will expose an [OpenAI Responses](https://platform.openai.com/docs/api-reference/responses/create)-compatible subset as its primary application protocol. An optional [A2A](https://github.com/a2aproject/A2A/blob/main/docs/specification.md) adapter may project the same deployments as discoverable remote Agents after the core task, artifact, and push-notification semantics are implemented. [MCP](https://modelcontextprotocol.io/specification/2025-06-18/server/index) remains a protocol for exposing tools and resources to an Agent, not the primary protocol for invoking a hosted Agent.

The internal Execution Service remains private. The serving gateway resolves an Agent reference to one immutable AgentVersion, constructs the complete internal execution specification from trusted platform state, persists the accepted Response, and submits only the exact snapshot to Execution Service. Public callers cannot override the harness, model connection, system prompt, built-in tools, Skills, secret references, workspace, or Executor.

The implemented [release registry foundation](../../implemented/architecture/2026-09-08-serving-release-registry-foundation.md), [Responses serving MVP](../../implemented/feature/2026-09-09-responses-serving-mvp.md), [stateful continuation](../../implemented/feature/2026-09-09-stateful-response-continuation.md), [strict structured output](../../implemented/feature/2026-09-09-structured-response-output.md), [frozen rich input](../../implemented/feature/2026-09-09-frozen-response-input.md), and [caller function round trip](../../implemented/feature/2026-09-09-caller-function-round-trip.md) supply the local Deployment, release history, rich invocation, authentication, policy, persistence, recovery, version-pinned multi-turn, schema-enforced output, function, and approval slices. The remaining requirements describe the broader lifecycle work.

## Product requirements

| ID | Requirement | Required behavior |
|---|---|---|
| R1 | Agent lifecycle | AgentVersion has explicit `draft`, `published`, and `retired` states; only a published, runnable version can receive a release alias. |
| R2 | Stable deployment | A tenant-scoped Deployment names one Agent family and owns `production`, `staging`, and custom aliases without changing immutable versions. |
| R3 | Exact resolution | The gateway resolves an alias once at request acceptance and records the requested reference, exact AgentVersion, and release revision before execution. |
| R4 | Public invocation | Clients can create, stream, retrieve, and cancel Responses through an authenticated API described by OpenAPI 3.1. |
| R5 | Conversation continuity | A continuation retains the original AgentVersion unless the caller explicitly starts a new conversation or requests a supported migration. |
| R6 | Isolation | Every invocation uses the existing Execution Service, isolated worker, private workspace, scoped credentials, and deployment-owned tool and network policy. |
| R7 | Release evidence | Promotion records the exact evaluation Runs, DatasetVersions, EvaluatorVersions, metrics, actor, reason, and policy result used for the decision. |
| R8 | Governance | API keys, tenant scope, deployment scope, rate limits, concurrency, budget, retention, artifact access, and audit export are independently configurable. |
| R9 | Observability | Every Response correlates tenant, deployment, requested alias, release revision, AgentVersion, conversation, Execution attempts, usage, cost, and terminal status. |
| R10 | Feedback | Authorized production samples can enter a review queue and become new versioned dataset cases without mutating historical Runs or invocation records. |

### Quality and security requirements

- An idempotency key is scoped to tenant and endpoint. Reusing it with a different normalized request fails instead of returning or starting unrelated work.
- Alias promotion uses compare-and-swap against the current release revision. Rollback creates a new revision that points to an older exact AgentVersion; history is never rewritten.
- Unknown or unsupported Responses fields fail with a stable `unsupported_parameter` error. The compatibility endpoint never silently ignores a requested behavior.
- Provider credentials remain in the Execution Service credential store. Public requests, Response records, events, logs, and artifacts never contain raw credentials.
- Public streams contain only supported Responses events. Native worker events and privileged traces remain behind deployment-scoped observability APIs.
- Artifact downloads use tenant-scoped authorization or short-lived signed URLs. The existing internal `executionId + path` reader is not a public artifact API.
- Admission applies request-size, rate, concurrency, and cost controls before starting a worker. Overload returns an explicit retryable failure and does not create unbounded queues.
- Every model-visible input, tool result, and continuation item is reconstructable from the durable conversation and Response records.

### Initial non-goals

- The compatibility endpoint does not expose arbitrary model provider calls or allow callers to mutate an AgentVersion at invocation time.
- `latest` does not mean the newest draft, and creating a version does not move `production`.
- An evaluation Run is not a production Response, and an Execution record is not the public lifecycle object.
- Full byte-for-byte support for every OpenAI Responses field, built-in tool, and event is not required for the first release.
- Multi-Agent comparison remains a Rank RunGroup concern. Public serving invokes one resolved Agent deployment per Response.

## Version and release semantics

The public `model` field carries an Agent reference because established Responses clients require that selector. A tenant resolves `<handle>@v17` as an exact immutable version. `latest`, `production`, and other names are DeploymentAlias records whose current head identifies an append-only ReleaseRevision. Publishing an AgentVersion advances the system-managed `latest` alias; it never moves `production`. Handles and aliases are tenant-scoped.

The accepted Response returns the exact resolved reference in `model`. Response headers also expose the requested reference and, for alias resolution, its release revision so a standard SDK can remain compatible while operators can prove routing decisions. Exact-version references have no synthetic release revision. Retries reuse the persisted exact version. `previous_response_id` continues the same conversation and version even when an alias advances; explicit conversation migration is a separate audited operation.

Promotion to `production` requires a release policy result. The first implementation may allow an authorized manual override with a required reason, but the record must distinguish a passed policy from an override. Automated policies can require named evaluation suites, minimum valid-trial coverage, pass-rate or reliability thresholds, maximum cost or latency, and freshness relative to the AgentVersion content digest.

## Public protocol

The implemented compatibility endpoint supports `model`, text, image and file `input`, caller function `tools`, scoped approval, `stream`, `background`, `metadata`, policy-controlled `instructions`, durable `store: true`, `previous_response_id` continuation, and strict `text.format` JSON Schema output. `max_output_tokens` enters only when its semantics can be reconstructed from durable records and enforced by Deployment policy.

```json
{
  "model": "customer-support@production",
  "input": [
    {
      "role": "user",
      "content": [
        { "type": "input_text", "text": "Review refund request 123" }
      ]
    }
  ],
  "stream": true,
  "metadata": { "order_id": "123" }
}
```

| Operation | Purpose |
|---|---|
| `POST /v1/responses` | Resolve a deployment, persist the accepted Response, and start or enqueue execution. |
| `GET /v1/responses/{id}` | Return the current durable Response projection and terminal output. |
| `POST /v1/responses/{id}/cancel` | Request cancellation without deleting audit history. |
| `GET /v1/responses/{id}?stream=true` | Resume a deployment-scoped event stream from a cursor when the standard create stream is no longer attached. |

The compatibility stream maps durable events to supported Responses event names such as `response.created`, output-item additions, text deltas, tool-call items, and terminal completion or failure. DSH commentary, native CLI messages, worker retries, and sandbox diagnostics remain internal observability data unless a standard output item represents them without losing meaning.

## Core flow

[Open the Agent release and serving flow diagram](../../../../rank/docs/diagrams/agent-serving-lifecycle.html).

1. Rank evaluates an exact AgentVersion and stores EvaluationEvidence that identifies the frozen DatasetVersion, EvaluatorVersion, Runs, metrics, and content digest.
2. The Release Controller evaluates policy, appends a ReleaseRevision, and compare-and-swaps the DeploymentAlias head. A direct client-to-Execution path is rejected at the public trust boundary.
3. The Serving Gateway authenticates the caller, applies tenant and deployment limits, and resolves the requested alias from the committed release registry.
4. The gateway atomically persists the Response request hash, requested reference, release revision, exact AgentVersion, and idempotency key before dispatch.
5. Execution Service persists its internal Execution and starts an isolated worker with server-constructed configuration and scoped credentials.
6. Durable Execution events update the Response projection. The gateway emits only compatible public events and records usage, cost, artifacts, and the terminal state.

## Data model

[Open the Agent serving entity model](../../../../rank/docs/diagrams/agent-serving-data-model.html).

| Entity | Ownership and invariants |
|---|---|
| AgentFamily | Tenant-scoped handle and descriptive identity; owns immutable versions. |
| AgentVersion | Frozen harness, model reference, system prompt, tools, Skills, and policy digest; lifecycle state changes do not mutate its execution specification. |
| EvaluationEvidence | References exact Runs, DatasetVersions, EvaluatorVersions, metrics, freshness digest, and policy outcome for one AgentVersion. |
| Deployment | Publicly callable name, owning AgentFamily, retention, quota, tool, network, and authorization policy. |
| DeploymentAlias | `latest`, `staging`, `production`, or another routing lane; its current revision pointer is the committed read projection. |
| ReleaseRevision | Append-only alias change with exact AgentVersion, actor, evidence, reason, expected predecessor, and activation time. Rollback appends another revision. |
| Conversation | Durable continuation context pinned to a Deployment and exact AgentVersion; every newly accepted Response has one. |
| Response | Public lifecycle record containing request hash, requested alias, resolved version and release revision, status, output items, usage, cost, and conversation reference. |
| Execution | Private attempt lifecycle and native artifacts; one Response can reference several attempts while retries preserve the same AgentVersion. |

## Core technical decisions

### Control plane and data plane

Agent authoring, evaluation, release policy, alias promotion, API-key administration, and audit search belong to the control plane. Request admission, alias reads, Response persistence, streaming, and execution submission form the production data plane. The first implementation may deploy these roles in one binary, but package and repository dependencies must preserve the ownership split so production traffic does not depend on loading an experiment workspace.

### Registry ownership

AgentFamily and AgentVersion cannot remain Rank-only concepts once production serving depends on them. Extract their Repository and application ownership into a product Agent Registry that both Rank and the Release Controller consume. Rank continues to own DatasetVersion, EvaluatorVersion, Experiment, Run, RunGroup, and Trial policy.

### Identifiers and pagination

Physical tables use an identity `BIGINT` primary key for joins, indexes, and keyset cursors. Public resources also carry a unique UUIDv7 identifier; only that opaque identifier crosses API, event, artifact, and service boundaries. SQLite uses `INTEGER PRIMARY KEY`, while the PostgreSQL adapter uses generated identity columns. Existing Rank string identifiers migrate when Agent Registry ownership is extracted rather than through an in-place compatibility layer.

List APIs use keyset pagination over an indexed stable tuple such as `(created_at, id)`. Ordered event streams and release histories use scoped integer sequences. Random UUID order and deep `OFFSET` are not pagination mechanisms.

### Trusted execution construction

The Serving Gateway accepts only the public input items and deployment selector. It loads the exact AgentVersion and Deployment policy, then constructs the internal execution specification. This prevents public callers from selecting credentials, weakening isolation, changing the system prompt, mounting undeclared tools, or reading arbitrary artifacts.

### Durable event projection

Response is the public source of status and output items; Execution is the private source of worker lifecycle. A projector folds ordered Execution events into ordered Response events with a persisted cursor. Reconnect starts from that cursor, and a terminal Response is reconstructable without an active worker or stream.

### Conversation ownership

A conversation owns an ordered input and output item history and is pinned to one exact AgentVersion. One live writer owns each conversation. Every newly accepted Response has a Conversation; omitting `previous_response_id` starts one, while supplying it resolves through the prior Response and rejects cross-tenant or incompatible Deployment continuation.

### Evaluation-governed release

Release policy consumes immutable EvaluationEvidence rather than querying mutable experiment selections. Policy evaluation and alias append occur in one transaction or through an outbox with idempotent commit. A failed policy produces an auditable decision but cannot advance the alias; an authorized override is explicit and cannot masquerade as a pass.

## Delivery phases

| Phase | Scope | Main tasks | Exit condition |
|---|---|---|---|
| 0. Domain foundation | Separate reusable Agent lifecycle state from Rank evaluation state. | Add AgentVersion lifecycle and digest; extract Agent Registry ownership; add Deployment, DeploymentAlias, ReleaseRevision, Response, API credential, and audit repositories; define branded public identifiers, internal identity keys, keyset cursors, and migrations. | Rank still creates and evaluates exact Agent versions through the new registry, and no serving endpoint exists. |
| 1. Safe serving MVP | Serve text-only stateless Agent calls through a Responses-compatible subset. | Add gateway authentication and scopes; exact, `latest`, and `production` resolution; idempotent Response creation; sync, SSE, background, retrieve, and cancel; quotas; signed artifacts; manual promotion with evidence and override records. | An external OpenAI-compatible client can call a production alias, and every accepted request proves its exact version, release revision, Execution attempts, usage, and cost. |
| 2. Stateful Agent interaction | Support durable multi-turn and richer input and output. | Add Conversation and `previous_response_id`; image and file items; structured output; caller function tools and tool-result continuation; approval-required status; webhook delivery. | A disconnected client can resume one conversation and complete tool or approval round trips without version drift or lost durable events. |
| 3. Automated release operations | Connect evaluation policy to controlled traffic changes. | Add named release policies; automatic staging promotion; policy freshness checks; canary targets and traffic weights; health rollback; live sampling and review-to-dataset flow; release dashboards and alerts. | A version can move from evaluation to canary and production under recorded policy, and rollback preserves complete history. |
| 4. Ecosystem protocols | Expose the same deployments to Agent ecosystems. | Add A2A Agent Card and task adapter; push notifications; TypeScript and Python SDK helpers; conformance fixtures; enterprise identity and audit export adapters. | Responses and A2A calls resolve the same release revision and produce equivalent Response, Execution, usage, and audit lineage. |

Each phase updates the public OpenAPI document, package and product READMEs, architecture references, TypeScript and Python client projections where applicable, unit and integration tests, and a keyless assembled transcript or protocol snapshot for user-visible behavior. Public protocol fixtures must cover cancellation, reconnect, idempotency conflict, alias movement during an active request, authorization denial, quota rejection, worker retry, and artifact access.

## Alternatives considered

**Expose Execution Service directly.** This would reuse an existing HTTP endpoint but expose privileged configuration and internal lifecycle vocabulary, omit tenant and release semantics, and couple external clients to a request designed for trusted Rank callers.

**Add production invocation as another Rank Run.** A Run freezes a DatasetVersion, EvaluatorVersion, trials, and quality aggregation. A production Response has different authorization, latency, conversation, retention, idempotency, and output semantics; merging them would weaken both records.

**Use only A2A.** A2A provides discovery, task, artifact, stream, and push-notification semantics for Agent-to-Agent collaboration, but ordinary applications more commonly need a direct request API and existing model-client integration. It remains an adapter over the same serving domain.

**Use MCP as the serving protocol.** MCP exposes tools, resources, and prompts to a model host. Treating a complete deployed Agent as a tool would hide its release, conversation, task, usage, and production lifecycle behind a narrower abstraction.

**Implement a custom public protocol first.** A custom schema could mirror internal DSH events exactly but would impose new client work and encourage public leakage of worker details. A strict Responses-compatible subset provides an established envelope while unsupported behavior fails explicitly.

**Resolve `production` for every retry or turn.** This would let one accepted request or conversation cross versions after a promotion. Resolution is therefore frozen at acceptance and inherited by continuation.

## Acceptance criteria

- Root product documentation presents AgentKiln as an Agent lifecycle platform while distinguishing shipped evaluation behavior from the proposed serving plane.
- The control-plane model supports immutable Agent versions, explicit publication state, append-only release revisions, evaluation evidence, and auditable overrides.
- Public requests cannot select internal credentials, harness configuration, system prompts, built-in tools, Skills, workspaces, Executors, or raw artifact paths.
- Exact-version, `latest`, `production`, retry, rollback, and continuation tests prove that every accepted Response remains pinned to one AgentVersion and, when resolved through an alias, one release revision.
- The documented Responses subset works through an OpenAI-compatible client with stable unsupported-parameter errors and reconnectable ordered events.
- Tenant isolation, API-key scope, idempotency conflicts, quotas, cost attribution, signed artifact access, secret redaction, cancellation, and crash recovery have integration coverage.
- Promotion policy consumes immutable evaluation evidence, blocks stale or failing evidence, records manual overrides, and preserves complete alias history.
- Responses and evaluation Runs remain separately queryable while sharing exact AgentVersion and Execution references.

## Risks

OpenAI-compatible SDKs may assume fields or stream events outside the declared subset. The gateway must publish a compatibility matrix, reject unsupported parameters, and run conformance fixtures against the supported SDK versions.

Extracting Agent Registry ownership changes Rank persistence and API dependencies before serving creates user value. Phase 0 must preserve existing Rank behavior and avoid a second writable copy of AgentFamily or AgentVersion.

Long-lived conversations create worker ownership, retention, context growth, and deployment-upgrade pressure. The design pins a conversation to one exact version and requires one live writer, but compaction and migration policy still need explicit limits before Phase 2.

Tools can perform irreversible external actions. Deployment policy, scoped credentials, sandbox and network restrictions, approval events, and audit records reduce risk but do not make every tool safe; operators remain responsible for the tools they publish.

Serving traffic changes availability and cost expectations. Production adapters need durable repositories, object storage, queue backpressure, worker leases, rate limits, and operational SLOs before a public deployment; the local SQLite and filesystem assembly remains a development configuration.
