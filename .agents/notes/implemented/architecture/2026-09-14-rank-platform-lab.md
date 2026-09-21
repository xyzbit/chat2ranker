# Agent Note: Complete Rank Kubernetes lab

Status: implemented

English | [中文](2026-09-14-rank-platform-lab.zh.md)

## Problem

Execution-only fixtures cannot establish that Rank's published endpoints, structured conversations and persistent control host work together after process replacement. A reproducible cluster must exercise the same public clients and packaged Harness used by the platform.

## Decision

The [complete lab](../../../../rank/deploy/k8s/README.md#complete-platform-experiment) runs the web gateway, Rank, control host and real DSH workers with PostgreSQL/River and S3. The dependency-only [runtime root](../../../../apps/rank-runtime/README.md) declares required workspace peers. Staging materializes dependency links and verifies Linux native archives so images do not depend on the developer checkout or macOS modules. Image tags identify the staged content, including binaries, runtime, web assets and manifests copied into images.

Rank HTTP replicas use the [rolling API lifecycle](../bug-fix/2026-09-17-rank-api-rolling-drain.md), while the dispatcher and control host each retain a single owner with `Recreate`. Rank business and login records use a PostgreSQL schema; control sessions and private webhook secrets use separate PVCs. Workers share durable jobs and objects with independent temporary workspaces. Full maintenance upgrades stop producers and drain consumers before the ordered Execution/Rank migration Job; that path has an explicit interruption even though schema-compatible HTTP replacements can roll independently.

The gateway protects management and static routes with a private lab Basic credential, leaves public endpoint bearer authentication to Rank, and hides internal control commands. Port-forwards bind to loopback. The [database privilege decision](2026-09-15-rank-database-permissions.md) isolates schema owners, application logins and control credentials. This trusted single-operator topology does not establish production identity, tenant separation or hostile-code isolation.

An explicit maintenance option selects the [OpenSandbox backend](2026-09-15-execution-opensandbox.md). Its Docker lifecycle service runs outside minikube, and complete Harness containers run outside the trusted Worker Pods. The same platform verifier checks remote allocation identity and S3-backed results. This topology validates integration on one laptop, not a Kubernetes-native sandbox controller.

## Alternatives considered

**Stop at an Execution fixture.** Demonstrates delivery but omits Rank's durable dispatch, endpoint API and separate control conversation.

**Install the developer checkout inside each image.** Retains host links and native binaries, increases the deployment payload and weakens reproducibility. A declared runtime closure makes packaging failures observable before rollout.

**Treat a shared worker Pod as a sandbox.** Resource limits bound a trusted lab process pool; they do not isolate jobs or hide the worker's credentials from an untrusted runtime.

## Consequences

The [acceptance script](../../../../rank/deploy/k8s/verify-platform.mjs) runs existing daily-news and OpenAI SDK chat clients, native structured history, SSE replay and S3 evidence reads. It replaces Rank/control Pods and verifies serving continuation and control-session resumption. With workers at zero, an accepted request survives Rank replacement, completes at attempt one after scale-up, and retains old artifacts. It restores two worker replicas on failure or success.

The [recorded results](../../../../rank/deploy/reports/2026-09-14-platform.md) use deterministic model responses and a source snapshot, not live news or external model quality. Browser acceptance covers two trial turns, reload recovery, Agent-to-endpoint navigation, integration, settings and documentation. Physical-host capacity, production authentication, complete runtime isolation and automatic scaling have separate acceptance requirements in the [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md).
