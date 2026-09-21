# Agent Note: Rank UI interaction

Status: implemented

English | [中文](2026-09-10-rank-ui-interaction.zh.md)

## Problem

Dense controls, inconsistent typography, and permanent internal scrollbars compete with conversation content. Automatic scrolling during updates interrupts readers inspecting earlier messages. Menus and forms need predictable keyboard behavior in narrow workspaces.

## Decision

Shared neutral surfaces, restrained accent colors, control dimensions, and readable secondary text align the management and conversation pages. Rank, trial, and recent-session regions hide scrollbars and show fades only on edges with unseen content. Other scroll regions use a thin thumb visible on hover or keyboard focus, with transparent tracks and no borders or arrow buttons. Resize and content observers refresh fade indicators; tail following depends on the reader remaining near the bottom. Forced-colors mode restores scrollbars and removes masks.

Native modal dialogs own focus containment and dismissal. Searchable model menus use native popovers to avoid clipping inside scrollable panels, keep options keyboard selectable, and consume Escape before the enclosing inspector. Single-choice fields follow the [shared controls decision](2026-09-10-rank-shared-controls.md). Forms and text remain opaque; only fixed mobile navigation uses translucency.

The [streaming and inspection decision](2026-08-24-rank-streaming-control-and-inspector.md) continues to own conversation events and workspace responsibilities. The [multi-Agent decision](2026-08-25-rank-multi-agent-comparison.md) continues to own selection persistence and run grouping.

## Alternatives considered

**Always scroll to the newest update.** This interrupts reading earlier messages when execution progress arrives. Following only near the tail preserves both live monitoring and inspection.

**Translucent forms and broad shadows.** These reduce text contrast and add competing layers to work-focused pages. Subtle translucency is limited to navigation that overlays scrolling content.

**Absolute-positioned model menu.** It is clipped by the inspector's scrolling container. A native popover retains the form relationship while rendering above that container.

## Consequences

Shell language switching uses the existing icon library's translation symbol, with a localized accessible name and tooltip.

Hidden scrollbars remove a drag affordance in normal color mode; edge fades, wheel, touch, and keyboard scrolling remain available. Native dialog and popover behavior requires a modern browser. Browser regression checks run against the assembled Rank UI and capture responsive screenshots; transcript snapshots cannot represent scroll position, focus, or clipping.
