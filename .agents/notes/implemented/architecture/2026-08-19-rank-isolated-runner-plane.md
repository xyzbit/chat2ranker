# Agent Note: Rank owns orchestration over isolated harness workers

Status: implemented

English | [中文](2026-08-19-rank-isolated-runner-plane.zh.md)

## Problem

Rank needs the complete DSH conversation runtime without making the control conversation the execution environment of the Agent under test. Candidate cases must not share context, files, credentials, or native history with each other or with their evaluator. Rank also needs one business record that can compare different harnesses while retaining each harness's native diagnostics.

## Decision

`apps/rank-web` hosts the persistent Control DSH Session and the Rank-specific control tools. Those tools call the versioned Go API and cannot start tested processes. The browser starts a run only through a signed structured action.

The Go `rankd` process owns immutable dataset and Agent snapshots, run state, case concurrency, judging policy, aggregation, and artifact authorization. Its application layer depends on Rank Repository interfaces and the versioned Execution Service client rather than DSH packages or process launchers.

The evaluation policy is an immutable `EvaluatorVersion` attached to the DatasetVersion and frozen into every Run. A Run expands into one `RunItem` per case and one `RunTrial` per Case × Trial pair; the default is five independent Trials per case. Required deterministic criteria execute before any LLM evaluation. A deterministic miss is a valid quality failure and spends no Judge tokens. Candidates that pass the gate are evaluated one Rubric criterion at a time, with frozen weights, critical flags, per-criterion thresholds, Judge harness, and Judge model.

Every candidate and Judge invocation crosses the versioned Execution Service HTTP contract. The independent `executiond` process persists an Execution record through its own Repository before the selected Executor starts a fresh `execution-worker` with a private workspace, private Harness Home, and execution identifier. The worker resolves a public `harness.Adapter` registry. The local Executor supports the deterministic mock and the repository's built DSH CLI directly. Pi, Claude Code, Codex, and Hermes use the same shell-free argument-array adapter. The Judge is a second Execution and receives only the case criteria and candidate output.

Rank derives Rubric pass/fail from the frozen score threshold instead of trusting an unconstrained Judge boolean. Candidate output is untrusted delimited evidence in the Judge prompt. Malformed or `unknown` Judge output is a grading failure, not a quality failure. Exhausted candidate execution errors are infrastructure failures. Only valid quality Trials enter the pass-rate denominator. Rank separately exposes reliable cases (all scheduled Trials valid and passing), pass^3, candidate cost, evaluation cost, invalid counts, and `evaluationComplete`.

Execution lifecycle and Harness progress are persisted as a per-execution ordered event log. The Execution Service client replays that log from sequence zero and follows its SSE endpoint so fast completions cannot race subscription. `rankd` maps candidate and Judge events into its own ordered Run event log; the browser follows Rank SSE and reconnects from the last persisted cursor. Native process streams never bypass either owner.

An immutable Agent version contains the Runner type, preset, system prompt, model, tool identifiers, and Skill references. Rank freezes these fields into every Execution specification. The DSH Adapter materializes model and system-prompt overrides as an invocation-local patch, while external command adapters receive explicit argument placeholders.

The local product stores Rank business state and generic Execution state in separate SQLite databases behind separate Repository interfaces. Execution Service owns the filesystem Artifact Store. Worker workspaces are removed after quiescence; requests, results, stdout, stderr, traces, and native Harness Homes remain addressable through references recorded on the Execution result. Rank stores only Execution and Artifact references. Production deployment may supply PostgreSQL repositories and Docker, Kubernetes Job, Kata, or remote Sandbox Executors without changing Rank domain code or the Execution Service contract.

## Alternatives considered

**Run tested DSH Agents inside Control DSH.** This provides the shortest call path but shares the plugin tree, Session persistence, environment, and failure domain with the product conversation, so the evaluation is contaminated and one tested process can damage control state.

**Keep the in-process Go DemoRunner as the execution architecture.** It is useful for narrow application tests but cannot prove process cleanup, credential reduction, separate harness homes, native artifacts, or independent Judge visibility.

**Keep execution lifecycle inside `rankd`.** This removes one local service but couples Rank business persistence and availability to Sandbox, Harness, and Artifact concerns, and prevents the execution plane from being reused by other products.

**Make DSH schedule Rank.** DSH owns conversation and tool-loop semantics, not version freezing, idempotent runs, cross-harness policy, or business persistence. Giving it orchestration ownership would couple product state to one harness and make peer adapters secondary.

## Consequences

The product has a larger local process graph and builds DSH libraries plus three Go binaries before startup. Five default Trials multiply candidate and Judge work, so the confirmation UI must show Trial count and estimated cost before dispatch. Two SQLite files and two durable event logs require independent migration, retention, and backup policy. In return, Control DSH keeps its native capabilities, Rank remains the authority for evaluation, deterministic assertions reduce Judge spend, operational failures cannot silently pollute quality metrics, Execution Service is reusable outside Rank, reconnecting clients can recover progress, and every candidate and Judge is independently attributable. Local process isolation is the first Executor; stronger container or cluster isolation remains a deployment choice behind the same contract.
