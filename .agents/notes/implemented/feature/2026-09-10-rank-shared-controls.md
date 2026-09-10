# Agent Note: Rank shared controls

Status: implemented

English | [中文](2026-09-10-rank-shared-controls.zh.md)

## Problem

An individually styled deployment menu leaves settings, filters, and forms using inconsistent native menus. Repeated focus rules and page selectors make a local visual fix change unrelated controls. Repeated option lists can diverge between connection editors.

## Decision

`ManagementUI.jsx` owns page headings, error notices, status pills, empty states, pagination, and inline confirmations. Page modules import these presentation components directly. Pagination receives availability and callbacks; confirmations receive labels and actions. Requests, cursor history, and pending mutations remain in each page.

`SelectField` owns every fixed-choice dropdown. Its props carry the controlled value, value callback, accessible label, and options. Radix owns keyboard navigation and focus management. A disabled empty option becomes the required-field placeholder; optional empty choices remain selectable. Menus mount inside the nearest native dialog when present, and Escape closes the menu before its parent.

`theme.css` owns visual tokens; `controls.css` owns control and focus styling; `ui-config.js` owns shared menu geometry. Pages own placement and business state. Language choices belong to i18n; provider and protocol choices belong to model-connection code. Backend catalogs, credentials, and per-form drafts do not become UI constants. Editable model inputs and searchable model discovery retain their distinct interactions.

This supersedes only the native single-select choice in the [UI interaction decision](2026-09-10-rank-ui-interaction.md), which remains authoritative for scrolling, modality, and translucency.

## Alternatives considered

**Style each page's dropdown independently.** The same keyboard, disabled, and focus behavior then needs repeated maintenance, and local changes do not fix the other pages.

**Only style the native select trigger.** The browser-owned popup still varies between platforms, leaving the discrepancy visible when the user opens it.

**Move all values into one global configuration object.** Provider catalogs and form drafts have different owners and lifetimes. Combining them with visual tokens obscures which changes affect presentation and which affect saved experiments.

## Consequences

Shared styling changes apply across settings, setup, Agent forms, deployment forms, and filters. Empty-value translation and dialog portal ownership require explicit browser regression coverage. Page rules exclude shared selects without raising specificity, so legacy list-row styles keep their precedence. Assembled desktop/mobile browser checks cover selection, disabled placeholders, resettable filters, model-connection drafts, nested Escape behavior, and modal menu placement.
