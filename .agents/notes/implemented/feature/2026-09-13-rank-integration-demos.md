# Agent Note: Rank Integration Demos

Status: implemented

English | [中文](2026-09-13-rank-integration-demos.zh.md)

## Problem

API fragments do not establish that a published Agent works in an application. News demos can misrepresent model memory as live data, and browser-only integrations risk exposing credentials.

## Decision

The [Responses example](../../../../rank/examples/responses-client/README.md) provides one synchronous headlines request and a streaming chat application using the official SDK. News collection precedes the model request and preserves timestamps and links. An empty or unavailable source prevents model invocation.

The loopback demo owns an HttpOnly visitor cookie and an in-memory transcript with the latest completed Response ID. Credentials stay in the server environment. Cancellation explicitly calls Rank; failed or cancelled continuations require a new conversation. Marked parses browser Markdown and DOMPurify sanitizes it.

Standalone ZIPs reuse these sources through an explicit Vite import allowlist and the fflate archive library. Only endpoint and platform settings enter `.env.example`; users add keys locally. The two packages select an initial demo and distinct ports while sharing runtime code and acceptance checks. Bundled readmes avoid checkout-relative dependencies, and verification evidence stays inside the extracted folder. Lazy loading keeps archive sources out of the initial application download.

## Alternatives considered

**Model-only current news** cannot establish freshness. **Agent external-tool round trips** are useful but require more than the single Responses creation demonstrated here. **Browser SDK credentials** expose the key. **Production session infrastructure** adds unrelated deployment requirements to a local tutorial.

**Requiring a repository checkout** adds unrelated setup to an API tutorial. **Recursive archive collection** can include local credentials or artifacts; a source allowlist fixes archive contents. **Separate copied implementations** would drift, so both downloads reuse the tested runtime.

## Consequences

Cancellation re-reads and retries when a concurrent queued-to-running transition invalidates its status update. Returning the newly running Response would acknowledge the request without stopping its execution. A terminal result wins without being overwritten. A deterministic race regression checks both outcomes, while live downloaded-demo acceptance exercises immediate cancellation through the platform proxy.

[Invocation feedback](../../../../rank/docs/response-feedback.md) connects application Response IDs to diagnostic details and reviewed dataset capture. Operators author expectations and redact standalone text explicitly; copying actual output as truth would propagate the original failure into evaluation. Cases retain source references, and appends create immutable versions with stale-version and duplicate-source rejection. Automatic conversation, attachment and tool replay is deferred because standalone case execution cannot reproduce those environments. The keyless assembled Rank/Execution scenario checks capture, evaluation and frozen Run data; browser acceptance covers search, refresh, review and exact evaluation handoff.

The examples are teaching applications, not public multi-user hosting. Sessions expire after 24 hours or restart, while Rank records persist. Keyless tests exercise the assembled demo HTTP server and official SDK through a deterministic Responses transport and transcript snapshot. Real acceptance covers live collection, recall, persisted output, version pinning, isolation and cancellation; missing credentials fail rather than skip. Real model prose is not a deterministic snapshot.
