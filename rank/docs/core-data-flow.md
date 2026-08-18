# Core data flow

This reference defines the data exchanged while preparing and running an experiment.

## Prepare an experiment

1. The browser sends an ordinary chat message to the persistent Control DSH Session.
2. The Rank Skill uses Control DSH tools to query or mutate datasets and agent configurations through `rankd`.
3. `rankd` stores reusable immutable versions and experiment selections through its repository interface; the local assembly uses SQLite.
4. The Control DSH plugin renders dataset and agent choices as A2UI inside the conversation.
5. The composer continues to send chat messages. Only an explicit A2UI action starts a run.

## Start and execute a run

1. `rankd` receives `StartRun` with an experiment, dataset version, and agent version.
2. `rankd` freezes both version snapshots and creates one `RunItem` per case.
3. The scheduler emits one immutable `ExecutionSpec` per `RunItem`.
4. `rankd` starts a `rank-worker` process for the case; the worker creates a private workspace, artifact directory, and harness home.
5. The selected Runner starts its harness through a CLI or SDK inside that Sandbox.
6. The Runner retains its request, standard output, standard error, native harness home, trace, and terminal result under the execution artifact directory.
7. `rankd` receives the versioned worker response and creates an independent Judge execution with the case input, expected criteria, and candidate output.
8. The Judge runs in another worker process with a different workspace, harness home, and execution identifier.
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

Normalized run events cover case start, candidate output, Judge completion, artifact availability, completion, cancellation, recovery, and failure. Native model and tool events remain in the harness artifact so Rank does not erase runner-specific diagnostics.

The terminal result contains final output, duration, token usage when provided, cost plus an explicit cost-known flag, Judge verdict, execution identifiers, and artifact references. Normalized data supports comparison; the retained native trace supports diagnosis.

## Judge visibility

The Judge receives only the case input, rubric, permitted tested output, reference data, and explicitly selected trace fields. It cannot read the control conversation, other cases, mutable dataset drafts, or credentials. Judge execution uses a separate Sandbox and execution identifier.

The [standalone core data-flow diagram](diagrams/core-data-flow.html) shows these steps by owning runtime role.
