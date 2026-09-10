# Agent Note: Signed Webhook outbox

Status: implemented

English | [中文](2026-09-09-signed-webhook-outbox.zh.md)

## Problem

A background or disconnected Responses caller needs a reliable state-change notification without holding an SSE connection or polling. A callback URL supplied on each public request would bypass Deployment governance and create a direct SSRF surface. Sending a callback after committing a Response would also lose notifications when the process stops between those two writes.

## Decision

Webhook endpoints are tenant-owned control-plane resources scoped to one or more Deployments and an explicit event allowlist. Supported events are `response.completed`, `response.failed`, `response.cancelled`, and `response.requires_action`. Each endpoint requires a public HTTPS URL. Its Standard Webhooks `whsec_` HMAC key is returned once and retained through a credential-store port; SQLite stores only the opaque secret reference.

A subscribed Response transition appends one frozen thin event per endpoint in the same SQLite transaction as the Response projection and durable Response events. The payload contains an event ID, event timestamp and type, and the Response ID. A receiver uses its scoped serving credential to retrieve the Response rather than receiving output or input data implicitly.

The dispatcher claims due rows with an expiring lease and keeps the event ID stable across attempts. It signs the exact body bytes with HMAC-SHA256 over `webhook-id.webhook-timestamp.raw_body` and sends the Standard Webhooks headers. Only HTTP `2xx` completes a delivery. Redirects are not followed; network errors and other statuses persist an error and next-attempt time. Retry delays span multiple days, accept deployment configuration, and honor a longer `Retry-After`. HTTP `410 Gone` disables the endpoint.

Outbound requests disable environment proxies, require HTTPS, resolve all destination addresses before dialing, and reject private, loopback, link-local, or otherwise non-public IPs. A restarted dispatcher reclaims only expired leases, so a stopped process cannot strand a delivery and two dispatchers cannot normally send the same active claim. Receivers must still deduplicate by event ID because a network failure after receiver commit but before acknowledgment can produce at-least-once delivery.

## Verification

Rank tests cover one-time secret projection, filesystem permissions and create-only writes, endpoint and event validation, transactional outbox creation, exact signature verification, failed-attempt scheduling, successful retry, expired-lease recovery through a new Service instance, and rejection of private destinations. HTTP tests create a Deployment-scoped endpoint, complete a Response, and observe its durable outbox record. The web build covers the bilingual endpoint-management surface.

## Alternatives considered

**Accept `webhook_url` on every Response.** Rejected because destinations are deployment policy and credential state, not untrusted per-invocation input.

**Store the HMAC key in the Rank database.** Rejected because ordinary repository backups and projections must not contain reusable signing credentials.

**Send after the Response transaction without an outbox.** Rejected because process failure can permanently lose the notification.

**Follow HTTP redirects.** Rejected because redirects add SSRF and credential-disclosure risk and Standard Webhooks treats `3xx` as a failed delivery.

## Consequences

External systems can receive durable, signed, Deployment-scoped Response notifications and recover after Rank restarts. The local adapter uses SQLite leases and a private filesystem secret store. Manual replay, signing-key rotation, endpoint failure alerts, PostgreSQL queue ownership, and a dedicated egress proxy remain production work.
