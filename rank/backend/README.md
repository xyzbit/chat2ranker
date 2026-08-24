# Rank backend

English | [中文](README.zh.md)

This Go module produces one binary:

- `rankd`: Rank API, version freezing, case scheduling, judging policy, result aggregation, and business persistence.

The backend depends on Rank Repository interfaces and the versioned Execution Service client. It does not import DSH implementation packages, start harness processes, or call `ctx.agents.create()`.

`rankd` freezes DatasetVersion, AgentVersion, and EvaluatorVersion, then expands a Run into Case × Trial records. Each Trial submits a candidate invocation, runs required deterministic checks first, and submits independent Rubric Judge invocations only when needed. The default is five Trials per case. Rank keeps quality, infrastructure, and grading failures separate; aggregates valid-Trial pass rate, reliable cases, pass^3, and split candidate/evaluation cost; and stores only Execution and Artifact references.

Browser clients follow `GET /api/runs/{id}/events` instead of polling worker processes. The local persistence adapter is SQLite; PostgreSQL can implement the same Repository interfaces without entering Rank domain or application code.
