# Agent Note: Rank owns orchestration over isolated harness workers

Status: implemented

English | [中文](2026-08-19-rank-isolated-runner-plane.zh.md)

## Problem

Rank needs the complete DSH conversation runtime without making the control conversation the execution environment of the Agent under test. Candidate cases must not share context, files, credentials, or native history with each other or with their evaluator. Rank also needs one business record that can compare different harnesses while retaining each harness's native diagnostics.

## Decision

`apps/rank-web` hosts the persistent Control DSH Session and the Rank-specific control tools. Those tools call the versioned Go API and cannot start tested processes. The browser starts a run only through a signed structured action.

The Go `rankd` process owns immutable dataset and Agent snapshots, run state, concurrency, cancellation, recovery, aggregation, and artifact authorization. Its application layer depends on domain repository and Runner interfaces rather than DSH packages.

Every candidate and Judge execution crosses the versioned JSON worker protocol and receives a fresh `rank-worker` process, private workspace, private harness home, and execution identifier. The local launcher supports the deterministic mock and the repository's built DSH CLI directly. Pi, Claude Code, Codex, and Hermes use the same shell-free argument-array adapter. The Judge is a second execution and receives only the case criteria and candidate output.

The local product stores business state in SQLite and retained execution data in the filesystem artifact store. Worker workspaces are removed after quiescence; requests, results, stdout, stderr, traces, and native harness homes remain addressable through artifact references recorded on the case result. Production deployment may replace repositories and launchers without changing the domain or worker protocol.

## Alternatives considered

**Run tested DSH Agents inside Control DSH.** This provides the shortest call path but shares the plugin tree, Session persistence, environment, and failure domain with the product conversation, so the evaluation is contaminated and one tested process can damage control state.

**Keep the in-process Go DemoRunner as the execution architecture.** It is useful for narrow application tests but cannot prove process cleanup, credential reduction, separate harness homes, native artifacts, or independent Judge visibility.

**Make DSH schedule Rank.** DSH owns conversation and tool-loop semantics, not version freezing, idempotent runs, cross-harness policy, or business persistence. Giving it orchestration ownership would couple product state to one harness and make peer runners secondary.

## Consequences

The product has a larger local process graph and must build DSH libraries plus two Go binaries before startup. In return, Control DSH keeps its native capabilities while Rank remains the authority for evaluation, every case and Judge is independently attributable, and harness-specific failures remain diagnosable without becoming Rank's storage model. Local process isolation is the first launcher; stronger container or cluster isolation remains a deployment choice behind the same protocol.
