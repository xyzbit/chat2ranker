# Sandbox network enforcement — 2026-09-15

English | [中文](2026-09-15-sandbox-network.zh.md)

The dedicated Docker runtime passes native TCP/UDP egress checks and the seven complete-platform checks on minikube. [Machine-readable evidence](2026-09-15-sandbox-network.json) records matching runtime/server images, source hashes, storage limits and independent expiry. The platform runs real DSH processes against a deterministic local model; no external model calls are made.

| Check | Observed result |
| --- | --- |
| Reachable controls | Independent probes reach the TCP and UDP listeners outside the restricted runtime. |
| Destination restrictions | UID 1000 reaches the allowed IP and TCP port; a second IP, another TCP port and UDP are blocked. Target-side records contain no denied probes. |
| Protected startup | Missing policy or NET_ADMIN stops startup before the command daemon. PID 1, execd and the child process lack NET_ADMIN and NET_RAW after initialization; native firewall modification fails. |
| Lifecycle-service loss | After killing the service, allowed TCP remains reachable and forbidden TCP/UDP remain blocked. The service is restarted and test resources are removed. |
| IPv6 | Infrastructure inspection confirms IPv4 and IPv6 default-deny rules. External IPv6 traffic is not exercised. |
| Frozen policy | PostgreSQL retains a nonempty egress policy; a differently configured Worker cannot claim it. The complete-platform report confirms the accepted runtime plan contains the lab policy. |
| Existing platform behavior | Hotspot collection, structured multi-turn chat, trial, control conversation, S3/SSE, Pod replacement and queue recovery pass. Packaged remote snapshots pass with SQLite and PostgreSQL/River with two Workers. |
| Storage and expiry regressions | The same images pass writable-storage exhaustion and recovery. After lifecycle-service loss, the protected 25-second deadline stops a detached workload despite the provider's 90-second TTL. |

The platform lab permits TCP 8800 in `192.168.49.0/24` for temporary model/tool fixtures. The dedicated network check narrows its policy to one target address and port. Empty configuration denies new external connections; DNS has no implicit exemption. Runtime plans retain their submitted rules, so an operator must drain or reconcile queued work before changing Worker policy.

These measurements establish IP/transport enforcement on this laptop. They do not establish production destination authorization, incoming tenant isolation, dynamic domain filtering, TLS-content inspection, independent-host capacity or real provider RPM/TPM behavior. Storage remains ephemeral tmpfs charged to runtime memory. Reproduce using the [network tutorial](../k8s/README.md#runtime-egress); the [implementation decision](../../../.agents/notes/implemented/architecture/2026-09-15-sandbox-egress.md) explains ownership and protected initialization.
