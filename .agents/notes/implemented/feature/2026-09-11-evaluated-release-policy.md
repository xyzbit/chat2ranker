# Agent Note: Evaluated release policy

Status: implemented

English | [中文](2026-09-11-evaluated-release-policy.zh.md)

## Problem

A completed evaluation does not establish release quality. Without binding the dataset and checking measured results, callers can label a weak or unrelated Run as passed. Evidence can also expire between review and publication, and rejected attempts need durable explanation without creating successful release history.

## Decision

Agent details have one Publish entry. The operator API resolves matching deployments from tenant-scoped immutable release history and selects the newest match, so later alias movement cannot turn a previously published version into an unrelated creation flow. Missing matches open a prefilled creation form, followed by release review for the selected version. The independent Deployment navigation opens a list instead of selecting a detail implicitly.

Agent families reference optional default development and regression Dataset families. Dataset remains generic, and every release freezes its actual DatasetVersions. Deployments own numeric release rules separately from immutable execution limits. A durable task freezes comparable development evidence and runs regression by default before transactional promotion. Alias and policy revisions are checked again at publication. Failures preserve the alias and record rejected decisions; explicit override requires a reason and retains failed checks. A deliberate pre-confirmation skip records a distinct outcome. [Release workflow](../../../../rank/docs/release-policy.md) owns API and metric semantics.

## Alternatives considered

**Accept any completed Run.** Rejected because completion cannot establish a quality threshold or prevent an unrelated dataset from qualifying a version.

**Require every Trial to be valid.** Rejected as an unconditional rule because the configurable coverage threshold must remain meaningful. A 100% threshold provides that stricter policy when desired; zero valid Trials always blocks evaluated publication.

**Trust preflight or save rejection inside a rolled-back transaction.** Rejected because evidence can expire after review, policy and aliases can change, and rollback would erase the rejection record.

## Consequences

One Endpoint represents one stable access target and one current version; separate endpoints distinguish environments. Creation leaves it unpublished, and another Agent version cannot bypass release review. Existing alias records remain available as historical details. API paths and persisted Deployment identifiers retain their names. This removes routing choices from ordinary publication while preserving explicit API references.

Integration pairs key selection or inline creation with executable examples. A timestamped name and all existing endpoint/operation permissions reduce repeated configuration; the submitted allowlist is fixed and does not expand to future endpoints. Hash-only key storage cannot restore plaintext, so existing keys require their retained value. Only explicit command copying includes the key; coding-Agent handoff and browser storage never receive it. Additional instructions and caller functions are standard endpoint capabilities, with validation and caller execution retained. The local documentation center owns product examples, and keyless acceptance executes their shared generator.

Defaults reduce repeated selection without restricting dataset reuse. New drafts resolve the latest dataset version; existing tasks retain frozen versions and rules. Task/Run creation, cancellation, and final alias promotion each preserve transactional state. Conversation review references restore progress at its original event. Audit snapshots retain historical thresholds after edits. Rollback appends a release and preserves existing conversation pinning. Keyless assembled worker/Responses snapshots and desktop/mobile browser flows cover publishing and reload; focused service tests cover precision, restart, cancellation, idempotency, and conflicts. Operator authentication, weighted routing, and automatic rollback remain outside this decision.
