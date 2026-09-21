# Agent Note: Bounded writable storage in the Rank Sandbox lab

Status: implemented

English | [中文](2026-09-15-sandbox-storage.zh.md)

## Problem

Artifact export bounds do not constrain a running Harness that creates many files, writes outside its workspace or fills the container layer. OpenSandbox injects its runtime into a stopped container; Docker rejects that injection when the root filesystem is read-only.

## Decision

The dedicated Docker lifecycle service has a validated read-only Rank runtime profile. It uses the Docker API to make the image filesystem read-only, configure bounded tmpfs areas and shared memory, disable swap beyond the required memory limit and rotate local container logs. Writable storage is ephemeral and shares the container memory budget. Byte limits and inode allowances are deployment configuration; they are not user-selected artifact limits or additions to the public Execution API.

The DSH adapter supplies Node’s `--expose-internals` argument for both prompt and history launches. The packaged CLI configuration-watching service requires it even in a headless profile; the adapter cannot depend on the host shell supplying Node options.

The Rank image includes the pinned execd binary and its protected PID 1 launcher at build time. The profile skips runtime injection and rejects request volumes, extensions, network-policy sidecars, configured binds and image-declared volumes. These refusals prevent additional writable mounts or an unbounded isolation overlay. Images that do not implement the packaged Rank startup fail to start; no local or writable-root fallback exists. The version-checked OpenSandbox build patch fails on source drift.

The profile bounds regular writable file storage for the complete container, including native UID 1000 commands outside DSH's tool policy. Read-only image paths remain unavailable for writes; input, workspace and artifacts share one allowance. Temporary files and shared memory have separate limits. Log rotation bounds retained log files by threshold, not exact host-disk bytes including Docker metadata. Memory exhaustion can terminate a workload before its tmpfs byte cap; the existing failed/unknown-result and cleanup rules still apply.

## Alternatives considered

**Bound only artifact exports or individual files.** Neither constrains aggregate runtime writes, inode exhaustion or alternative writable directories.

**Use a writable root with a workspace tmpfs.** The container layer and image-declared volumes permit writes outside that mount. A read-only root and rejection of extra volumes make the allowed writable areas explicit.

**Add filesystem quota administration or a second Sandbox API.** The lab needs short-lived workspaces that fit in an assigned memory budget. Native Docker tmpfs limits satisfy that scope without a host-specific filesystem setup; persistent or larger workspaces require a separate storage design.

## Consequences

The [lab tutorial](../../../../rank/deploy/k8s/README.md#bounded-writable-storage) owns the configuration and commands. Its authenticated native-process experiment and checked-in snapshot require aggregate byte/inode exhaustion, denied root writes, rejected alternate mounts and successful writes after cleanup. The [report](../../../../rank/deploy/reports/2026-09-15-sandbox-storage.md) records image identities and platform regression evidence. Protected lifetime verification remains required because the image supplies its own launcher.

This is an enforced profile for the dedicated Docker lab, not a guarantee for arbitrary OpenSandbox installations, persistent volumes, hostile kernel exploits or production egress. The [egress implementation](2026-09-15-sandbox-egress.md) separately controls transport destinations. Production identities, host capacity and longer-lived workspace storage remain separate requirements in the [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md).
