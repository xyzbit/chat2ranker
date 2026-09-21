# Release workflow

English | [中文](release-policy.zh.md)

Dataset is a reusable versioned asset. An Agent family optionally references default development and regression Dataset families; these defaults are shared by every Agent version. A new evaluation with no selected dataset resolves the development default. Explicit workspace selections remain unchanged. Release drafts resolve the latest versions of both defaults, then retain those exact selections until replaced.

## Review and publish

The Deployment navigation opens a searchable list; rows open addressable details and Agent-version links return to their configuration. Agent details expose one Publish link. It opens the deployment with the most recent release of that exact version, including historical releases. With no match, creation preselects the Agent and suggests an available name. Creation continues to release review with the requested version fixed.

Each deployment presents one endpoint and one current published version. Separate named deployments distinguish test and production. Creation leaves the endpoint unpublished; creating an Agent version never changes its published target. Publication uses an internal `production` alias and requires the confirmation below. Existing alias references and history remain readable; ordinary callers use the stable deployment name as described in the [serving API](serving-api.md#deployment-reference).

The product calls these serving targets Endpoints. The overview displays execution limits; release revisions and check decisions appear directly in history. Published endpoints offer an Integration drawer with key selection or creation followed by executable cURL and JavaScript commands. Existing keys require their retained plaintext because storage contains only hashes. Commands include the selected key; Copy for Agent uses an environment variable instead. A link opens the local documentation center.

The deployment dialog and Rank conversation use the same release review. Development results compare the current and candidate Agent versions using the newest comparable completed Runs: identical DatasetVersion, frozen evaluator configuration (including Judge model), and repeat count. Missing results remain empty; they never imply a zero score or approval. Operators can select any DatasetVersion or run a fresh version comparison in an independent experiment.

Regression is enabled by default. Confirming creates a durable release task and, when enabled, a separate regression Run. The task freezes the candidate, current alias revision, selected DatasetVersions, development evidence, release rules, actor, and conversation review reference. The dispatcher waits for the regression Run to finish, validates its results, and appends the release atomically with task completion. Reloading or restarting resumes the recorded task. Regression never changes the source conversation's dataset or Agent selection.

A failed regression leaves the alias unchanged. Operators can review failed checks, prepare a new attempt, or explicitly continue with a nonblank reason in failure details. Turning regression off before confirmation records `regression_skipped`; it does not claim a passing evaluation. Cancellation persists task and Run cancellation together. Alias or policy changes during regression yield a conflict requiring a fresh review.

## Release rules

Deployment settings own numeric release rules, independently of Dataset defaults and immutable execution limits. Unconfigured tasks use 100% minimum pass rate, 100% valid Trial coverage, and 24-hour evidence validity. Saved rules permit pass rate 0–100%, coverage 1–100%, and a positive integer validity in hours.

Evidence must match the candidate Agent and selected DatasetVersion, complete every scheduled Trial, have a completion timestamp, and contain at least one valid Trial. Pass rate is `passed / validTrials`; coverage is `validTrials / scheduledTrials`. Checks use unrounded values: two passes out of three do not satisfy 67%. Invalid Trials reduce coverage. Future timestamps and expired evidence fail validation. Skipping regression and overriding failures remain distinguishable in audit records.

## Operator API

These endpoints belong to the trusted local operator service. External applications use the [Responses API](openapi.yaml). Operator authentication is required before exposing the control plane beyond its trusted local environment.

| Method | Path | Input or result |
|---|---|---|
| GET | `/api/deployments?agentVersionId={id}` | Deployments with releases of the exact version, newest matching release first; optional `tenantId` defaults to the configured tenant |
| GET / PUT | `/api/agent-families/{id}/evaluation-defaults` | Dataset family IDs; writes include `expectedRevision` |
| POST | `/api/deployments/{id}/release-draft` | Alias, candidate, optional development/regression DatasetVersion overrides; returns comparison and expected revisions |
| POST | `/api/deployments/{id}/release-comparison` | Draft selection; creates an independent comparison experiment |
| POST | `/api/deployments/{id}/release-tasks` | Reviewed selection, expected revisions and evidence Run IDs, `actor`, optional `runRegression`; requires `Idempotency-Key` |
| GET | `/api/release-tasks?deploymentId={id}` | Latest 100 tasks; `experimentId` alternatively scopes conversation tasks |
| GET | `/api/release-tasks/{id}` | Frozen review, regression Run reference, status and decision |
| POST | `/api/release-tasks/{id}/cancel` | Cancels a task that has not published |
| POST | `/api/release-tasks/{id}/override` | `reason`; only blocked tasks qualify |
| GET / PUT | `/api/deployments/{id}/release-policy` | Numeric rules; writes include `expectedRevision` |
| GET | `/api/deployments/{id}/aliases/{alias}/decisions` | Frozen decisions with cursor pagination |

Omitted dataset overrides resolve defaults; an explicit empty string clears that selection. Task submission includes `expectedReleaseRevisionId`, `expectedPolicyRevision`, `expectedCandidateRunId`, and `expectedCurrentRunId` from review. Changed review evidence returns 409. Replaying the same idempotency key and input returns the original task, including after publication; changed input returns 409.

The direct `aliases/{alias}/preflight` and `aliases/{alias}/releases` endpoints also remain available for explicit existing-Run evidence. That path requires a configured policy DatasetVersion for evaluated publication or a reasoned manual override. Both paths audit decisions and compare expected alias and policy revisions transactionally.

## Conversation and rollback

Rank can create a Deployment when its name and Agent are known, set Agent defaults, prepare publication, and read release status. The user starts publication through the confirmation action. Its durable task appears at that conversation event after reload. Multiple matching deployments require a target choice.

Rollback selects a previous immutable Agent version and creates another release task. New conversations resolve the new alias; existing conversations retain their exact Agent version and release revision. Weighted routing and automatic rollback are outside this workflow.

## Verification

`RANK_RELEASE_BROWSER=1 RANK_RELEASE_SDK=1 pnpm rank:test` runs Go tests, keyless assembled Control Host and worker/Responses snapshots, desktop/mobile browser flows, and the official OpenAI SDK example. It requires the Playwright CLI skill and installed [Responses client dependencies](../examples/responses-client/README.md). Temporary services and databases isolate verification from user data; screenshots stay under `output/playwright/`. Keyless workers verify orchestration. Real-provider quality and cost acceptance remain a separate [local acceptance](local-acceptance.md) flow.
