# Agent Serving API

English | [中文](serving-api.zh.md)

This reference defines the implemented local Agent serving data plane. The machine-readable contract is [OpenAPI 3.1](openapi.yaml); the broader lifecycle design remains in the [Agent lifecycle serving proposal](../../.agents/notes/proposed/feature/2026-09-08-agent-lifecycle-serving.md).

## Deployment reference

The `model` field is `<deployment>@<selector>`. A selector such as `v3` resolves the third immutable Agent version in the Deployment's Agent family. `latest`, `staging`, and `production` resolve the current DeploymentAlias head. Request acceptance persists the exact AgentVersion and, for an alias, its ReleaseRevision; a later promotion cannot change an accepted or replayed Response.

The version in `research-public@v3` is the Agent version, not the ReleaseRevision number. ReleaseRevision is an audit identifier for one alias movement and appears only in the returned `deployment.release_revision_id` when an alias was used.

## Authentication and admission

Every data-plane request uses `Authorization: Bearer rnk_live_...`. A credential has explicit `responses:create`, `responses:read`, `responses:cancel`, and `responses:approve` scopes plus an allowlist of Deployment identifiers. Cross-tenant or out-of-scope resources return `404` where revealing their existence would disclose another tenant's data.

Deployment policy enforces requests per minute, active Response concurrency, execution timeout, and whether caller `instructions` are accepted. Admission and Response creation share one SQLite transaction, so a rejected request does not start an Execution or consume an untracked queue slot.

`Idempotency-Key` is scoped to the API credential. Repeating the same normalized request returns the original Response and sets `X-Idempotent-Replayed: true`; reusing the key with different content returns `409 idempotency_conflict`.

Every accepted Response belongs to a durable Conversation pinned to one Deployment, AgentVersion, and optional originating ReleaseRevision. The Conversation head admits one writer at a time; concurrent or stale continuations return `409` without starting an Execution.

## Operations

| Method | Path | Scope | Behavior |
|---|---|---|---|
| `POST` | `/v1/responses` | `responses:create` | Create a synchronous, background, or streaming Response. |
| `GET` | `/v1/responses/{id}` | `responses:read` | Read the durable Response projection. |
| `GET` | `/v1/responses/{id}?stream=true` | `responses:read` | Replay and follow durable events. |
| `POST` | `/v1/responses/{id}/cancel` | `responses:cancel` | Cancel a non-terminal Response without deleting history. |
| `POST` | `/v1/responses/{id}/approval` | `responses:approve` | Approve or reject a pending caller function without executing it in Rank. |

The local control plane also creates Deployments, advances aliases, issues credentials, and manages Webhook endpoints under `/api`. Those operator endpoints are not part of the public compatibility API; a non-loopback deployment must place them behind the product's operator authentication and authorization layer.

## Request subset

| Field | Support |
|---|---|
| `model` | Required Deployment reference. |
| `input` | A non-empty string or an array of `user` messages containing `input_text`, `input_image`, and `input_file` items. |
| `stream` | Returns durable Responses-compatible SSE events. |
| `background` | Returns after persistence and dispatch instead of waiting for a terminal state. |
| `metadata` | Up to 16 string entries; keys are at most 64 characters and values at most 512 characters. |
| `instructions` | Accepted only when the Deployment policy enables it; appended to the versioned system prompt. |
| `store` | Omit or set to `true`; accepted Responses are always durable. |
| `previous_response_id` | Continue from the latest completed Response in the same Conversation. |
| `text.format` | Plain `text`, or strict `json_schema` with a named Draft 2020-12 schema. |
| `tools` | Up to 16 strict caller-owned function definitions when Deployment policy enables them. |
| `max_output_tokens` | Rejected by this version. |
| Other fields | Rejected instead of being ignored. |

The caller cannot select a model connection, provider credential, harness, preset, system prompt, built-in tools, Skills, workspace, Executor, or artifact path. Rank constructs those fields from the pinned AgentVersion and Deployment policy before submitting the private Execution.

## Example

```sh
curl http://127.0.0.1:8787/v1/responses \
  -H "Authorization: Bearer $RANK_SERVING_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: example-1" \
  -d '{"model":"research-public@production","input":"Summarize this deployment.","stream":false}'
```

A completed object includes standard Response fields plus a `deployment` extension containing the requested reference, resolution kind, exact AgentVersion, and optional ReleaseRevision. `cost.known` distinguishes a measured or configured amount from missing pricing data.

For structured output, set `text.format.type` to `json_schema`, provide a 1–64 character `name`, an object `schema`, and `strict: true`. External `$ref`, `$dynamicRef`, and `$recursiveRef` values are rejected. Rank persists the normalized format in the Response and private Execution specification; the worker validates the final output as exactly one JSON value before completion. Invalid harness output fails the Response instead of returning schema-breaking text.

## Image and file input

`input_image.image_url` accepts a base64 data URL or a public HTTPS URL; `detail` is optional and currently accepts only `auto`. `input_file` accepts either `file_url` or `file_data`. `file_data` may be raw base64 or a base64 data URL and requires `filename`; `file_id` is rejected because this API does not provide a Files resource.

Rank decodes inline content or downloads each remote source before admitting the Response, computes a SHA-256 content identifier, and persists the frozen bytes with the Response. A signed source URL is represented in the idempotency digest but is not persisted. Remote fetches disable proxies, allow at most three redirects, require HTTPS throughout, and reject private, loopback, link-local, or otherwise non-public resolved addresses.

Each request accepts at most eight binary items and 20 MiB of decoded content. A continuation carries every prior frozen attachment into its Execution, so the same limits apply to the Conversation's cumulative attachment set. The JSON request body is limited to 32 MiB. Execution materializes attachments under its private `.rank-inputs` workspace directory and retains copies as private artifacts; the public API does not expose raw worker paths or artifact URLs.

## Caller functions and approval

A `tools` entry uses the Responses function form with `type`, `name`, optional `description`, object `parameters`, and `strict: true`. The Deployment must set `allowCallerTools`. Rank persists the canonical definitions and adds them to the immutable model-visible execution prompt. If the Agent selects a function, Rank validates its name and argument object against the frozen definition and returns a `function_call` output item; Rank never executes caller-owned code.

Continue that Response with a top-level `function_call_output` item whose `call_id` matches the prior call and whose `output` is a non-empty string. The continuation inherits the same tool definitions and rejects attempts to replace them. The ordered Conversation records both the call and its result, so a restart or retry reconstructs the same round trip.

A function definition may set `require_approval: true`. Its first Response stops at `requires_action` with `approval_status: pending`. A credential with `responses:approve` calls `/v1/responses/{id}/approval`; approval completes the Response and permits a matching tool result, while rejection cancels it. Rank records the decision as a durable event and does not treat approval as permission to execute the function itself.

## Conversation continuation

Pass a completed Response identifier as `previous_response_id` to append another turn. Rank persists the predecessor link and Conversation head in the same admission transaction, reconstructs the ordered user and assistant history, and submits that history plus the Conversation's frozen image and file inputs to Execution Service.

```sh
curl http://127.0.0.1:8787/v1/responses \
  -H "Authorization: Bearer $RANK_SERVING_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: example-2" \
  -d '{"model":"research-public@production","input":"What should I do next?","previous_response_id":"resp_..."}'
```

The requested `model` must identify the same Deployment. Alias movement is ignored for version selection during continuation: the new Response inherits the Conversation's AgentVersion and ReleaseRevision and reports `deployment.resolution_kind: conversation`. An exact selector that names another AgentVersion fails with `400 conversation_version_mismatch`. A non-terminal predecessor fails with `409 previous_response_not_completed`, and a predecessor that is no longer the Conversation head fails with `409 conversation_head_changed`.

## Streaming and recovery

Create with `stream: true`, or retrieve with `GET /v1/responses/{id}?stream=true`. Each SSE record has an integer `id`, a Responses event name, and a JSON `sequence_number`. Reconnect with `starting_after=<id>` or `Last-Event-ID`; only later durable events are sent.

Success emits `response.created`, `response.in_progress`, output-item and content-part lifecycle events, one or more `response.output_text.delta` events, and `response.completed`. Failure emits `response.failed`; cancellation emits `response.cancelled`. Both terminal events carry the durable Response projection. Worker logs, retries, sandbox diagnostics, raw credentials, and internal Execution events are not projected into the public stream.

Rank resumes queued and in-progress Responses from its repository after restart. Submission uses a Response-derived idempotency key, so recovery reattaches to the same Execution rather than starting a second invocation.

## Signed Webhooks

Webhooks are configured on a Deployment through the control plane, not supplied by individual `/v1/responses` requests. `POST /api/webhook-endpoints` accepts a tenant, name, public HTTPS URL, one or more Deployment identifiers, and a subset of `response.completed`, `response.failed`, `response.cancelled`, and `response.requires_action`. The returned `whsec_...` signing secret is shown once. Rank stores only its opaque credential-store reference in SQLite; the local process provider writes the secret into a private `0700` directory with `0600` files.

Each event is a thin payload so the receiver retrieves the current Response with its own scoped API credential:

```json
{"id":"evt_...","object":"event","created_at":1788940800,"type":"response.completed","data":{"id":"resp_...","object":"response"}}
```

Delivery follows Standard Webhooks. Rank signs the exact bytes sent with HMAC-SHA256 over `webhook-id.webhook-timestamp.raw_body` and sends `webhook-id`, `webhook-timestamp`, and `webhook-signature: v1,<base64>`. Receivers must verify the raw request body before parsing JSON, reject stale timestamps, and deduplicate by `webhook-id`.

The Response state transition, durable event, and one outbox row per matching endpoint commit in the same SQLite transaction. A delivery succeeds only on HTTP `2xx`; redirects are not followed. Network errors and non-`2xx` responses retry on a configurable multi-day schedule, honoring a longer `Retry-After`. HTTP `410 Gone` disables the endpoint. Claims use durable leases, so another dispatcher or a restarted Rank process reclaims an attempt only after its lease expires. `/api/webhook-endpoints/{id}/deliveries` exposes status, attempts, next attempt, and the last error without payload or signing material.

Webhook egress requires HTTPS, disables environment proxies, resolves every destination address before dialing, and rejects private, loopback, link-local, or otherwise non-public IPs. Production should additionally isolate egress behind a filtering proxy or subnet policy.

## Limitations

This implementation uses the local SQLite repositories and direct Execution Service client. Webhook delivery uses single-database leases rather than a distributed queue. Conversation migration, manual Webhook replay and secret rotation, signed public artifacts, weighted canary routing, PostgreSQL persistence, distributed leases, and production operator authentication remain later phases of the lifecycle proposal.
