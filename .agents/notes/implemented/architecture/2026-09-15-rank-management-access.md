# Agent Note: Rank workspace operator access

Status: implemented

English | [中文](2026-09-15-rank-management-access.zh.md)

## Problem

The shared Rank workspace contains Agent definitions, datasets, model-connection management and publication controls. A gateway password alone leaves private Rank and control-host addresses unprotected, provides no application identity for publication, and does not constrain the control service's HTTP access.

## Decision

Installation operators authenticate with independent high-entropy tokens through webpage login or HTTP Bearer/Basic clients. A versioned operator file contains subjects, SHA-256 token digests and optional expirations. Rank reads the file for each request; atomic replacement supports overlapping rotation and revocation. Invalid or unavailable files reject management access with a dependency error rather than retaining stale authorization. Credentials and hashes do not enter API identity responses, conversation text or execution snapshots.

Operators administer the shared workspace, including its serving tenants. Agent, Dataset and model-connection definitions remain installation-wide. Public API credentials retain their existing tenant, endpoint and operation restrictions. An operator token cannot invoke the public Responses API; a serving key cannot administer Rank. This implements a controlled workspace, not team-isolated SaaS data ownership. Team isolation requires persistent ownership throughout those shared domain objects before granting team-scoped users access.

Non-loopback `rankd` listeners require an operator file. Enabling that file requires explicit internal secrets even with SQLite, so a known development control token cannot bypass operator authentication. Loopback-only development can omit it. The control host checks Rank's management-authentication configuration before binding a non-loopback listener, then authorizes each public session/message/stream request through Rank. The gateway serves the public login shell and forwards operator authorization and browser cookies; backend checks also protect direct private service access. Production browsers require a TLS gateway and the configured public origin on both services. Mutation requests reject other origins.

The control token remains a separate service identity. An explicit route allowlist covers experiment/bootstrap reads, evaluation defaults, non-secret model configuration and the existing internal command/transcript protocol. It cannot mint public API keys, read their management list, delete connections or provide model secrets. Internal control endpoints and runtime reload stay hidden at the gateway. Direct publication and release-task creation record the authenticated subject instead of trusting a caller-supplied actor.

Authorization is evaluated at request admission. Revoking an operator or public key prevents later requests without cancelling accepted work or retroactively terminating an established stream. Kubernetes Secret projection must reach the mounted file before a change takes effect; the cluster verifier waits for observed admission/revocation rather than assuming that a successful Secret update is immediate.

## Alternatives considered

**Only authenticate at Nginx.** Direct service access would retain a different security policy and publication identity would depend on a client-provided field.

**Build a full identity platform.** Lightweight browser sessions over random operator credentials support ordinary webpage login without owning password recovery or user enrollment. Database-backed sessions preserve login across API replacement without adding an identity service. Enterprise SSO can be adopted when required; this implementation makes no SSO or team-isolation claim.

**Give the control service an operator credential.** It would let model-driven tooling access secret-management operations. The explicit service allowlist preserves the existing no-secrets-in-chat rule.

## Consequences

The [minikube report](../../../../rank/deploy/reports/2026-09-15-management-access.md) records the installed images, observed authorization results and ten passing complete-platform checks.

[The packaged example](../../../../rank/examples/access-control/README.md) verifies actual Rank/Execution processes, operator rotation, invalid-file refusal, trusted publication identity and public tenant/endpoint/scope isolation. HTTP tests cover service restrictions, Basic identity mismatch and cross-origin mutations. The [platform verifier](../../../../rank/deploy/k8s/verify-platform.mjs) repeats public authorization through the gateway, replaces a projected operator allowlist, and checks direct control-host/service restrictions before the existing DSH conversation and recovery scenarios.

Operator provisioning and file ownership belong to deployment administration. Full audit logging, enterprise identity integration, team ownership, internal service-token rotation, workload identity and network isolation remain separate production work in the [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md).

Browser login uses opaque fixed-lifetime HttpOnly cookies, explicit same-origin mutations and server-side logout. Rank revalidates the current credential file on every cookie-authenticated request. The browser mounts business views only after identity verification and returns expired sessions to login while retaining the URL. The control host forwards cookies only to identity verification. Bounded database storage shares cookie and credential digests across processes under the [rolling API decision](../bug-fix/2026-09-17-rank-api-rolling-drain.md); raw credentials are not persisted. The [packaged example](../../../../rank/examples/access-control/README.md) verifies login, cookie writes, logout and credential revocation without external model calls.
