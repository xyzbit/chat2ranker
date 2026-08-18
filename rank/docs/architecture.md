# System architecture

chat2ranker separates conversational control, experiment orchestration, and tested harness execution into different runtime roles.

## Runtime roles

| Role | Process | Owns | Must not own |
|---|---|---|---|
| Rank UI | Browser | Conversation presentation and A2UI actions | Business persistence or Runner lifecycle |
| Control DSH | Node host | Persistent control Session, Skill execution, tool loop, and UI plugin runtime | Tested case execution |
| Rank Orchestrator | Go `rankd` | Dataset and agent versions, experiments, runs, scheduling, aggregation, and cancellation | Harness-specific process behavior |
| Sandbox Executor | Go `rank-worker` | One isolated local-process execution, with container and cluster launchers supplied by deployment adapters | Experiment decisions or scoring policy |
| Runner | Sandbox workload | One harness invocation and native trace | Rank database or Control DSH state |
| Judge Runner | Independent Sandbox workload | Rubric evaluation for one case | Other cases or the control conversation |

## Process relationship

Control DSH and a tested DSH runtime are separate instances. The Control DSH plugin calls `rankd` through a versioned API. `rankd` sends an immutable execution specification to `rank-worker`. The worker starts DSH, Pi, Claude Code, Codex, Hermes, or another harness through a peer Runner adapter.

For a DSH case, `ctx.agents.create()` executes only inside the isolated DSH Runner. The Control DSH process never creates the tested Agent. The two runtimes do not share a Cordis context, Session persistence, DSH home, workspace, environment, plugin instances, or credentials.

## Control ownership

- DSH controls conversation turns, model and tool loops, compaction, and durable control-session history.
- Rank controls dataset selection, agent-version selection, version freezing, run creation, concurrency, timeout, retry, cancellation, judging, and aggregation.
- A Runner controls only one assigned harness invocation.
- The Judge is dispatched by Rank through the same isolation model as tested cases.

## Data ownership

| Data | System of record |
|---|---|
| User and assistant conversation, tool calls, control-session events | Control DSH Session log |
| Dataset, DatasetVersion, Agent, AgentVersion, Experiment, Run, RunItem | Rank SQLite repository in the local product assembly |
| Frozen run input and evaluation result | Rank SQLite plus immutable artifact references |
| Native harness Session log, stdout, stderr, request, result, and generated files | Filesystem artifact store |
| Ephemeral workspace | Case or Judge Sandbox |
| Per-execution DSH home | Filesystem artifact store, isolated by execution identifier |

Rank records `Experiment.control_session_id`, `RunItem.agent_execution_id`, and `RunItem.judge_execution_id` to join business records with execution artifacts. DSH Session JSONL is not used as the Rank business database.

## Deployment resources

The local product assembly starts one Control DSH host, one `rankd`, and one `rank-worker` child process per candidate or Judge execution. SQLite stores business state, the filesystem stores artifacts and native harness homes, and each execution receives a private temporary workspace. A production deployment replaces the repository and process launcher with PostgreSQL, a queue, object storage, and container or cluster-job adapters without changing the Rank domain model or Runner protocol. Every DSH Session store has one live writer.

Horizontal scaling assigns each job and DSH Session to one worker owner. Rank state and job leases coordinate workers; shared direct writes to one DSH Session directory are forbidden.

## Related references

- [System architecture diagram](diagrams/system-architecture.html)
- [Core data flow](core-data-flow.md)
- [Repository structure](project-structure.md)
- [Control and execution separation](adr/0001-control-and-execution-separation.md)
