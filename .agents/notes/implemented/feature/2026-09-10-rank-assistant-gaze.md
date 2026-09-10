# Agent Note: Rank assistant gaze

Status: implemented

English | [中文](2026-09-10-rank-assistant-gaze.zh.md)

## Problem

Rank's assistant needs a recognizable, responsive expression without distracting readers or moving conversation content.

## Decision

The avatar uses a solid circle and two white eyes. Small periodic glances give it expression; nearby mouse movement takes priority. Motion stays inside the existing avatar dimensions. Reduced-motion preferences leave the face still, and touch input does not trigger tracking. Browser listeners and pending animation frames belong to the mounted avatar and are released on unmount.

## Alternatives considered

**Whole-body movement.** Tilting or bouncing the entire avatar attracts attention throughout a long conversation. Eye movement provides the requested response with less visual interruption.

**Continuous pointer tracking.** Following the mouse anywhere on the page makes distant conversation avatars react to unrelated work. A local radius limits that distraction.

## Consequences

The expression is decorative and does not encode execution state or change conversation events. Browser verification covers idle glances, nearby and distant pointers, touch input, reduced motion, and desktop/mobile rendering. Text transcript snapshots cannot capture gaze direction, so the browser smoke records a visual snapshot of the assembled conversation.
