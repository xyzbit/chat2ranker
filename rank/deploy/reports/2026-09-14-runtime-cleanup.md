# Runtime cleanup upgrade acceptance

English | [中文](2026-09-14-runtime-cleanup.zh.md)

The [sanitized report](2026-09-14-runtime-cleanup.json) records a maintenance upgrade of the existing `rank-dev` minikube platform from Execution PostgreSQL schema 1 to schema 2. Old gateway, Rank, control and Execution processes stopped before the ordered Execution/Rank migration Job. The platform restarted with two workers and retained its database, control PVCs and S3 artifacts. This procedure deliberately interrupts lab service; it is not a zero-downtime migration.

The full platform verifier passed daily-news and two-turn OpenAI SDK chat, structured history, direct trial filters, terminal SSE replay, S3 evidence reads, persistent control conversations, Rank/control Pod replacement, and a queued response surviving Rank restart and worker scale-to-zero/scale-up. The completed Execution had no pending runtime cleanup. Responses came from the deterministic local model service; news sources were injected fixtures, not live news.

Focused real PostgreSQL tests separately verified model and tenant capacity retention after unknown failure, stale confirmation rejection, premature completion/cancellation rejection and repeatable schema migration. SQLite and application tests covered the same cleanup state semantics. The packaged Execution lifecycle snapshot passed in SQLite and PostgreSQL/River modes, including running and cleared cleanup state.

This run did not add browser acceptance beyond the [earlier platform report](2026-09-14-platform.md). It does not prove whole-runtime isolation, automatic cleanup of lost local owners, production identity separation, autoscaling or physical cross-host execution. Those requirements remain in the [production proposal](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md).
