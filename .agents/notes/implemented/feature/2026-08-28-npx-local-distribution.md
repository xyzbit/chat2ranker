# Agent Note: npx local distribution

Status: implemented

English | [中文](2026-08-28-npx-local-distribution.zh.md)

## Problem

The source launcher compiled Go services and started Vite from the repository, so a user needed a clone, pnpm, and Go. Publishing that script directly through npm would also place durable databases and credentials inside npm's disposable cache.

## Decision

`@xyzbit/agentkiln` is a thin Node.js launcher. It downloads one versioned, SHA256-verified runtime archive for the host platform into `~/.agentkiln/runtime/`, supervises Execution Service, Rank, Control DSH, and the static Web UI, and keeps all durable state under `~/.agentkiln`. `--home` isolates another profile. Foreground start remains the default; detached start, status, open, and stop use one private PID file.

The runtime archive contains three statically compiled Go binaries, the production Web bundle, Rank skills, and the DSH runtime. Keeping DSH out of the thin npm launcher avoids making every user resolve its large dependency graph during `npx`. Execution workers receive the packaged DSH CLI path through `RANK_DSH_BIN`; source runs retain the repository-path fallback.

The packaged DSH runtime is fixed to the complete `0.1.0-rc.8` package family. The release manifest overrides transitive DSH workspace packages to that release and rejects a runtime containing another DSH version, so a later npm pre-release cannot silently create a mixed plugin graph.

`pnpm rank:package` builds the current platform runtime, checksum, and npm tarball under `dist/agentkiln/`. It prints local acceptance and external publishing commands but never publishes by itself.

The public product identity is AgentKiln (`agentkiln` in package, repository, launcher, and archive names; `AGENTKILN_*` in launcher environment variables). The name covers preparation, evaluation, release, and serving. Internal Rank service names, API headers, and resource identifiers remain stable. The default home is `~/.agentkiln`; existing profiles require an explicit `--home` after stopping the old launcher, as described in the [quick start](../../../../rank/docs/quickstart.md#existing-local-data). GitHub source publication does not publish npm packages or runtime archives.

## Alternatives considered

**Publish the complete runtime as npm dependencies.** This would make every `npx` invocation resolve the large DSH dependency graph and risk mixed pre-release packages, so the thin launcher instead installs one verified runtime archive.

**Keep source-only startup.** This preserves the shortest implementation but requires every user to install Go and pnpm and keeps the product unsuitable for a quick public trial.

**Use Docker as the only distribution.** Docker is useful for server deployment, but it adds a container prerequisite and filesystem concepts to the default local onboarding path.

## Consequences

Users need Node.js but do not need the source tree, Go, or pnpm. npm cache cleanup cannot delete experiments or credentials. Program upgrades replace immutable runtime versions while SQLite migrations upgrade durable data in place. A DSH upgrade is an explicit release-script change followed by packaged tool-call acceptance, rather than an incidental dependency resolution. Docker remains a separate server deployment option rather than the default local installation path.
