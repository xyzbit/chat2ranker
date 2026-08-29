# ADR 0001: Separate control and tested harness runtimes

Status: accepted

## Context

chat2ranker uses DSH for the product conversation while evaluating agents implemented with DSH and other harnesses. Running a tested DSH Agent inside the Control DSH process would give one harness different isolation, lifecycle, and state-sharing behavior from Pi, Claude Code, Codex, Hermes, and external runners.

## Decision

Control DSH hosts only the persistent experiment conversation, Rank Skill, tools, and A2UI. The Go Rank service owns experiment state and submits every tested harness invocation to a generic Execution Service. The Execution Service dispatches peer Harness Adapters through an isolated process, container, or cluster job.

A tested DSH runtime is a separate Execution Worker instance. `ctx.agents.create()` for a case runs inside that instance. The tested runtime never shares process memory, Cordis context, Session persistence, DSH home, workspace, environment, or credentials with Control DSH.

Judge execution follows the same process-isolation rule and cannot reuse the Control DSH Session.

## Consequences

- Runner comparison uses one execution and isolation model across harnesses.
- Harness versions and agent configurations can be frozen independently from the product runtime.
- Failures and global plugin state in a tested harness cannot corrupt the control conversation.
- The platform requires a versioned Execution Service contract, Repository, Executor, Artifact Store, Harness Adapter, and event normalization.
- Local development starts more processes than an in-process DSH-only prototype.

## Alternatives considered

**Run DSH cases inside Control DSH.** This reduces initial code but creates unequal isolation, permits shared state, and couples product uptime to tested code.

**Use DSH as the scheduler and business database.** DSH owns Agent execution and Session history, but it does not own dataset versioning, experiment transactions, cross-harness scheduling, or aggregate evaluation results.

**Run every interaction through a generic shell command without an Execution contract.** This launches processes but cannot provide stable idempotency, cancellation, usage accounting, artifacts, or comparable terminal states.
