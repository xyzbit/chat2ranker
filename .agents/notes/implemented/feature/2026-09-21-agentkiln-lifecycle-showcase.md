# Agent Note: AgentKiln lifecycle showcase

Status: implemented

English | [中文](2026-09-21-agentkiln-lifecycle-showcase.zh.md)

## Problem

An experiment-centered landing page does not explain how to create, try, deliver, and integrate an Agent. Visitors need the whole workflow before evaluating its individual tools.

## Decision

The [product README](../../../../README.md) follows creation, conversation trials, evaluation comparison, endpoint publication, and integration. A silent HyperFrames video and five actual interface screenshots present that sequence. Short Chinese captions keep the video understandable without audio; the English README describes the same capabilities.

Screenshots retain real UI state. The comparison example uses the deterministic Demo Runner and is explicitly labeled as example data. Its rates and costs illustrate the interface rather than evidence of model quality. API examples show an environment-variable reference instead of a credential.

## Alternatives considered

**Keep the experiment demo as the primary introduction.** It explains evaluation but leaves the creation and delivery paths undiscovered.

**Reconstruct idealized product screens.** Such graphics can imply controls or completed operations that the application does not provide. Actual screens keep the explanation tied to available behavior.

## Consequences

UI changes can make screenshots and video stale; updates require recapture, HyperFrames layout checks, and visual inspection of key moments and transitions. The documentation checks validate links and bilingual structure, not the depicted product behavior. Existing experiment footage remains available as historical media.
