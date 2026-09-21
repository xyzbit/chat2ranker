# Deployment

English | [中文](README.zh.md)

Local development runs Control DSH, `rankd`, `executiond`, `execution-worker`, SQLite business repositories and filesystem artifacts. Execution also supports PostgreSQL/River, independent API/worker processes and S3-compatible artifacts; [its README](../../execution/backend/README.md) owns configuration and failure semantics. [Rank business storage](../backend/README.md) supports SQLite or an explicitly migrated PostgreSQL schema; its coordinators and control sessions retain a single-owner deployment.

Control DSH, Rank, Execution and harness workloads use separate persistent resources and credentials. Put product APIs behind authentication and a reverse proxy; DSH trusted-host checks are not authentication. Execution service tokens protect internal calls but do not authenticate product users.

The [runnable lifecycle example](../examples/execution-lifecycle/README.md) verifies packaged Execution processes locally. The [production design](../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md) owns the remaining sandbox and scaling work. Its [local Kubernetes experiment](../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md#local-kubernetes-implementation-experiment) defines minikube acceptance. Cluster bootstrap alone does not prove application deployment or cross-host capacity.

The [lab tutorial](k8s/README.md) provides the complete minikube startup path. The [Execution report](reports/2026-09-14-execution.md) records process/S3 and fixture scaling evidence; the [platform report](reports/2026-09-14-platform.md) records DSH endpoint, trial, browser and restart-recovery results.
