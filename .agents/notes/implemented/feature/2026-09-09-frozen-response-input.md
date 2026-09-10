# Agent Note: Frozen Response image and file input

Status: implemented

English | [中文](2026-09-09-frozen-response-input.zh.md)

## Problem

The public Responses endpoint accepted text only. Adding image and file fields without freezing their content would make retries, Conversation continuation, and historical replay depend on mutable remote URLs. Letting an Execution Worker download caller URLs would also bypass serving admission limits and grant network access at a more privileged lifecycle stage.

## Decision

`POST /v1/responses` accepts `input_image` with a base64 data URL or public HTTPS `image_url`, and accepts `input_file` with one public HTTPS `file_url` or base64 `file_data`. Inline file data requires `filename`; image `detail` defaults to and only supports `auto`; `file_id` is rejected until a Files resource owns that identifier.

Rank freezes every binary input before Response admission. It decodes inline data or downloads remote content through a proxy-free HTTPS client that validates each redirect and rejects private, loopback, link-local, and other non-public resolved addresses. Rank computes the content SHA-256 identifier, persists the bytes and normalized media metadata with the Response, and retains only a digest of a remote source URL for idempotency.

Execution protocol version 2 carries content-addressed `inputAttachments`. The service validates item count, decoded size, media type, filename, supported detail, and content digest before persistence and again inside the worker. The worker materializes each attachment under `.rank-inputs` in its private workspace, retains an artifact copy, removes attachment bytes and credentials from `request.json`, and tells the Agent which immutable paths to inspect.

A request accepts at most eight binary items and 20 MiB of decoded content. A continuation reconstructs prior user and assistant messages and supplies every frozen attachment in Conversation order. The same limits therefore apply to the cumulative attachment set required by one continued Execution.

## Verification

Contract tests cover validation and deterministic worker paths. Execution service and worker tests cover invalid digests, private materialization, artifact retention, and redacted request records. Rank HTTP tests cover mixed text, image and file input, SQLite persistence, remote-source freezing, idempotent replay without a second download, URL redaction, unsupported forms, private-address rejection, and attachment-preserving continuation. The assembled serving snapshot sends inline image and file input through Rank, Execution Service, and a real worker process.

## Alternatives considered

**Pass remote URLs to the Worker.** Rejected because the referenced bytes could change after admission and Worker network policy would become part of public input fetching.

**Persist only an object-storage reference.** Deferred because the local assembly has no durable object-store adapter. The Response repository owns frozen bytes in this implementation, while the Execution contract remains content-addressed and can move to object-backed storage without changing the public protocol.

**Support `file_id` as an arbitrary string.** Rejected because no Files resource currently authenticates ownership, freezes content, or defines retention for that identifier.

**Keep only the latest turn's attachments.** Rejected because a continued Agent must receive the same model-visible inputs that its durable Conversation history describes.

## Consequences

An accepted Response and every continuation execute against immutable image and file bytes even when the caller used an expiring or mutable URL. Local SQLite size grows with retained inputs, request admission performs bounded network I/O for remote sources, and long Conversations can reach the cumulative attachment limit. Production deployment still requires object storage, retention policy, malware and content scanning, and signed artifact access.
