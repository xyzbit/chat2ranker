# Deployment

English | [中文](README.zh.md)

Local development runs Control DSH, `rankd`, `executiond`, `execution-worker`, two SQLite repositories, and filesystem artifact storage. Production deployment can replace each Repository with PostgreSQL and assign each invocation to an isolated container, Kubernetes Job, Kata runtime, or remote Sandbox.

Control DSH, `rankd`, `executiond`, and Harness workloads use separate persistent resources and credentials. Raw DSH Web access must sit behind product authentication and a reverse proxy; DSH trusted-host checks are not an authentication layer.
