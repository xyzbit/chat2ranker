# Agent Note: Frozen Sandbox egress and protected network initialization

Status: implemented

English | [中文](2026-09-15-sandbox-egress.zh.md)

## Problem

Private Docker endpoints and read-only filesystems do not constrain outbound traffic. A Harness can open sockets without using a configured HTTP proxy or a DSH tool. Network restrictions also need to survive lifecycle-service loss and remain consistent with accepted work.

## Decision

Execution freezes a normalized transport allow list in `RuntimePlan.egress` before queuing. The operator configures IPv4/IPv6 CIDRs, TCP/UDP and destination ports through `-sandbox-egress-policy` or `EXECUTION_SANDBOX_EGRESS_POLICY`. Empty policy denies new external connections. Port zero permits every port of the selected protocol. Unknown JSON fields, invalid prefixes/protocols and port overflow reject configuration; rules are owned copies, sorted and deduplicated. A Worker with a different policy cannot claim the queued request. Request replay preserves its original policy and attempt.

The dedicated Docker profile invokes the packaged `network-init` entry point in a private network namespace. It grants NET_ADMIN only for protected startup. Before exposing execd, PID 1 installs IPv4 and IPv6 filter tables using pinned iptables tooling, with default-deny OUTPUT and FORWARD. Local loopback and replies to established/related connections remain allowed. Installation failure or missing policy stops the container; there is no proxy-only or local fallback.

PID 1 then replaces itself through `setpriv`, removing NET_ADMIN and NET_RAW from the bounding set and clearing inheritable/ambient capabilities. Reexec replaces every Go runtime thread. PID 1, the root command daemon and its descendants cannot regain these capabilities. The original absolute lifetime still applies. Kernel rules remain installed if the lifecycle service disappears; no independent firewall sidecar can remove them while the workload survives.

This policy controls destination IP prefixes and transport ports, not domain names, HTTP paths or the content sent to an allowed service. DNS is not implicitly exempted. Operators using hostnames must explicitly allow the resolver and destination addresses; changing DNS answers requires a reviewed policy update or a separately managed egress gateway. Policy changes are an idle maintenance operation: accepted work retains its policy, and mismatched Workers leave it waiting rather than widening access. Inbound service authorization remains separate from outbound enforcement.

## Alternatives considered

**Only set proxy environment variables.** Native sockets can ignore them. Enforcement belongs to the network namespace that contains the complete runtime.

**Adopt OpenSandbox's egress sidecar immediately.** Its maintained DNS/nftables implementation remains useful for dynamic domain policies, but the pinned Docker integration requires host-port handling, a shared writable volume and independent sidecar cleanup. The initial fixed-destination policy uses kernel filters with the existing protected runtime lifecycle. Domain filtering and credential interception are not recreated here.

**Grant NET_ADMIN throughout execution.** A privileged command daemon or descendant could alter rules. The grant is restricted to startup and removed through a process replacement before accepting commands.

## Consequences

[Configuration and experiments](../../../../rank/deploy/k8s/README.md#runtime-egress) describe the dedicated lab policy. The [network report](../../../../rank/deploy/reports/2026-09-15-sandbox-network.md) distinguishes actual IPv4 TCP/UDP measurements from inspection of IPv6 rules. Reachable control targets, target-side arrival records, dropped process capabilities and lifecycle-service loss are required evidence; a client timeout alone cannot prove enforcement. Parser and queue tests reject invalid policy and changed-policy Workers, and PostgreSQL tests retain the added plan fields.

The lab permits only TCP 8800 in its dedicated Docker subnet so temporary deterministic model/tool fixtures remain usable. This is a laboratory policy, not a production tenant policy. Production needs narrowly assigned destinations, scoped identities and host capacity validation. Dynamic DNS/domain authorization, incoming tenant isolation and TLS-content inspection are not provided by this transport policy. The [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md) retains those deployment distinctions.
