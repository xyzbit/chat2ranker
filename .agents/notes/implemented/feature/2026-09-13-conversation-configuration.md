# Agent Note: Conversation Configuration Forms

Status: implemented

English | [中文](2026-09-13-conversation-configuration.zh.md)

## Problem

Configuration available only through management navigation interrupts the Rank conversation. Giving the control model credential-bearing API responses would persist secrets in its session and expose them to subsequent model calls.

## Decision

Direct Rank tools handle non-secret dataset versions, Agent versions, model metadata, bindings, evaluation defaults, endpoint creation and publication thresholds. A durable `a2ui/open_configuration` event exposes a typed configuration action for secure forms and settings review. The action stores a section and resource references; reopening reads current state. It does not authorize or imply a save. Unknown fields are rejected before the command and event are stored.

The conversation reuses platform forms in a native dialog. Secret creation and entry stay in browser memory and deterministic API calls. Closing discards plaintext. Webhook configuration shares one component between the endpoint page and conversation. Numeric publication policies use the same validation and transaction helper in direct API and control-command paths.

Visual hierarchy uses neutral surfaces, restrained action colors, typography and spacing. Essential validation, immutable-limit notices and secret-retention information remain visible; repeated page explanations are omitted.

On mobile, the companion occupies a top-toolbar slot beside history and documentation so it cannot cover form actions. Desktop retains the floating launcher.

## Alternatives considered

**Credential mutation tools returning API responses** would expose API keys or signing secrets in durable model-visible history.

**Links to management pages for every setting** would require users to leave the conversation and reconstruct the requested context.

**A separate set of conversation forms** would duplicate validation and let permission defaults and save behavior diverge from management pages.

## Consequences

Configuration remains operable through conversation while sensitive operations require deterministic forms. Simple non-secret changes execute directly. Browser-owned preferences and files still require the browser. Historical actions reopen current configuration, not a historical editable snapshot. Keyless Control Host transcripts cover tool execution and event persistence; browser acceptance uses temporary services for real key, Webhook and policy mutations and desktop/mobile form behavior.
