# Core data flow

This reference defines the data exchanged while preparing and running an experiment.

## Prepare an experiment

1. The browser sends an ordinary chat message to the persistent Control DSH Session.
2. The Rank Skill uses Control DSH tools to query or mutate datasets and agent configurations through `rankd`.
3. `rankd` stores mutable drafts and immutable versions in PostgreSQL.
4. The Control DSH plugin renders dataset and agent choices as A2UI inside the conversation.
5. The composer continues to send chat messages. Only an explicit A2UI action starts a run.

## Start and execute a run

1. `rankd` receives `StartRun` with an experiment, dataset version, and agent version.
2. `rankd` freezes both version snapshots and creates one `RunItem` per case.
3. The scheduler emits one immutable `ExecutionSpec` per `RunItem`.
4. `rank-worker` leases the job and creates a case-specific Sandbox.
5. The selected Runner starts its harness through a CLI or SDK inside that Sandbox.
6. The Runner streams normalized execution events and persists the native trace and generated artifacts.
7. `rankd` records usage, duration, cost, final output, and terminal execution state.
8. Rank creates an independent Judge execution with the case input, rubric, permitted output, and reference answer.
9. Rank stores the score and reason, aggregates the run, and publishes run-state events.
10. Control DSH projects those events into the experiment Session; the browser updates the A2UI result card.

## Execution input

| Field group | Examples | Rule |
|---|---|---|
| Identity | run, run item, case, execution | Opaque versioned identifiers |
| Harness | type, version, image, entry point | Frozen before dispatch |
| Agent | prompt, model, tools, Skill references, limits | Immutable `AgentVersion` snapshot |
| Case | input, attachments, expected behavior, rubric | Immutable `DatasetVersion` snapshot |
| Environment | workspace artifact, network policy, scoped secret references | Materialized only in the Sandbox |
| Limits | timeout, cost budget, output retention | Enforced by worker and Runner |

## Execution output

Normalized events cover messages, model calls, tool calls, tool results, usage, artifacts, standard output, standard error, completion, and failure. Each event carries an execution identifier and monotonic sequence number.

The terminal result contains final output, exit code, duration, token usage, normalized cost, terminal status, and native trace reference. Normalized data supports comparison; the unmodified native trace supports diagnosis.

## Judge visibility

The Judge receives only the case input, rubric, permitted tested output, reference data, and explicitly selected trace fields. It cannot read the control conversation, other cases, mutable dataset drafts, or credentials. Judge execution uses a separate Sandbox and execution identifier.

The standalone visual is stored in `rank/docs/diagrams/core-data-flow.html` after the project diagram profile is selected.
