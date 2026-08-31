# Agent Note: Use-case-first README demo

Status: implemented

English | [中文](2026-08-31-use-case-first-readme-demo.zh.md)

## Problem

The product README explains the evaluation workflow before naming the decisions Chat2Ranker helps users make. Static screenshots also require readers to reconstruct how conversation, run confirmation, comparison, and the side workspace connect.

## Decision

The root README introduces four concrete uses before installation: comparing model versions, comparing Harnesses, comparing Agent configurations, and detecting regressions. Each use names the variables that remain fixed and the metrics available for comparison.

A short product-native film demonstrates one experiment from a stated goal through data and Agent preparation, explicit run confirmation, result comparison, and side-workspace inspection. The README embeds a high-resolution poster linked to the 1080p MP4 and removes screenshots that repeat the same sequence.

## Alternatives considered

**Keep only feature-oriented prose.** A feature inventory describes available components but makes readers translate those components into their own evaluation decision.

**Keep separate screenshots for every step.** Static images are easier to update independently but make the workflow longer to scan and duplicate the animation.

**Embed an animated GIF.** GitHub renders it reliably, but GIF palette conversion and README downscaling make small interface text visibly soft. A lossless-looking poster linked to the MP4 preserves legibility.

## Consequences

Readers can identify a relevant use before installing the package and can see the complete interaction model without reading architecture documentation. The repository carries two generated media files, and material workflow changes require refreshing both assets so the README does not present stale UI.
