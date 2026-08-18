# System architecture

chat2ranker separates conversational control, experiment orchestration, and tested harness execution into different runtime roles.

## Runtime roles

| Role | Process | Owns | Must not own |
|---|---|---|---|
| Rank UI | Browser | Conversation presentation and A2UI actions | Business persistence or Runner lifecycle |
| Control DSH | Node host | Persistent control Session, Skill execution, tool loop, and UI plugin runtime | Tested case execution |
| Rank Orchestrator | Go `rankd` | Dataset and agent versions, experiments, runs, scheduling, aggregation, and cancellation | Harness-specific process behavior |
| Sandbox Executor | Go `rank-worker` | Job lease and isolated process, container, or cluster-job lifecycle | Experiment decisions or scoring policy |
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
| Dataset, DatasetVersion, Agent, AgentVersion, Experiment, Run, RunItem | Rank PostgreSQL |
| Frozen run input and evaluation result | Rank PostgreSQL plus immutable artifact references |
| Native harness Session log, stdout, stderr, generated files | Artifact store |
| Ephemeral workspace and harness home | Case Sandbox |

Rank records `Experiment.control_session_id`, `RunItem.agent_execution_id`, and `RunItem.judge_execution_id` to join business records with execution artifacts. DSH Session JSONL is not used as the Rank business database.

## Deployment resources

The first deployment contains one Control DSH host, one `rankd`, one or more `rank-worker` processes, PostgreSQL, artifact storage, and isolated Runner sandboxes. A local worker may use child processes; production workers should use containers or cluster jobs. Every DSH Session store has one live writer.

Horizontal scaling assigns each job and DSH Session to one worker owner. Rank state and job leases coordinate workers; shared direct writes to one DSH Session directory are forbidden.

## Related references

- [Core data flow](core-data-flow.md)
- [Repository structure](project-structure.md)
- [Control and execution separation](adr/0001-control-and-execution-separation.md)

Standalone architecture and data-flow diagrams are stored in `rank/docs/diagrams/` after the project diagram profile is selected.
