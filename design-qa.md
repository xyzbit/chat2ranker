# Design QA: Agent-first control plane vertical slice

## Evidence

- Source visual truth: `/Users/staff/.codex/generated_images/01a07ee8-977e-7282-9815-5b9f62ef870f/exec-a6ec64b7-d358-4f69-a1e7-68bfe45fef6e.png`, `/Users/staff/.codex/generated_images/01a07ee8-977e-7282-9815-5b9f62ef870f/exec-20778b72-2a71-4b2f-bfa3-cd1877ae436d.png`, and the sidebar reference `/var/folders/4q/x_g69pmj73n9tt7dl7f61x7r0000gp/T/codex-clipboard-9ba4c00d-2e9a-4d58-90f3-162e426ed28b.png`.
- Browser-rendered implementation: `/tmp/chat2ranker-agent-release-final-v2.png`, `/tmp/chat2ranker-deployment-final.png`, `/tmp/chat2ranker-deployment-mobile-final.png`, `/var/folders/4q/x_g69pmj73n9tt7dl7f61x7r0000gp/T/chat2ranker-sidebar-redesign-2026-09-09/02-desktop-final.png`, and `/var/folders/4q/x_g69pmj73n9tt7dl7f61x7r0000gp/T/chat2ranker-sidebar-redesign-2026-09-09/03-mobile-final.png`.
- Combined comparison evidence: `/tmp/chat2ranker-agent-final-comparison.png`, `/tmp/chat2ranker-deployment-comparison.png`, `/tmp/chat2ranker-release-card-focus-comparison.png`, and `/var/folders/4q/x_g69pmj73n9tt7dl7f61x7r0000gp/T/chat2ranker-sidebar-redesign-2026-09-09/04-reference-comparison.png`.
- Desktop viewport: 1486 x 1059 CSS pixels at browser density 1. Source Agent image: 1486 x 1059 pixels. Source Deployment image: 1487 x 1058 pixels. Implementation captures: 1486 x 1059 pixels. The one-pixel Deployment difference was padded with white during comparison; no scaling was applied to the full-view comparisons.
- Mobile viewport: 390 x 844 CSS pixels at browser density 1. Implementation capture: 390 x 844 pixels.
- State: Chinese and English shell, Agent conversation with a manual `production` release from Agent v1 to v2, release success, Deployment route state, truthful empty traffic, release dialog, navigation tooltip, and compact mobile Deployment table.

## Findings

No actionable P0, P1, or P2 differences remain. The implementation preserves the selected information architecture, spacious neutral palette, inline A2UI release confirmation, current-to-candidate arrow, and card-owned primary actions. The latest follow-up replaces the desktop icon rail and task list with one AI-native sidebar: page navigation above recent chats, with New chat as an action rather than a Workspace destination.

- [P3] The implementation uses the repository's Inter/system-font stack instead of a separately bundled display face. The weight, tracking, hierarchy, and Chinese fallback remain optically consistent with the source; adding another font dependency is not justified for this slice.
- [P3] The release card omits pass-rate, latency, and cost deltas when no completed evaluation exists. This is intentional product behavior: the interface shows em dashes and requests a manual-override reason instead of fabricating metrics.
- [P3] The Deployment page omits the source's persistent assistant column and traffic chart. This follows the later approved interaction rule: assistant progress stays in conversation, side detail opens only when a card requires it, and traffic remains empty until real requests exist.

## Required fidelity surfaces

- Fonts and typography: verified hierarchy, truncation, weight, line height, bilingual wrapping, and increased release-card metric labels after the first pass.
- Spacing and layout rhythm: verified the 252-pixel unified sidebar, page-to-history separation, single-scroll recent-chat list, 752-pixel A2UI card column, card padding, table rhythm, and mobile single-column policy layout.
- Colors and visual tokens: verified warm neutral background, white surfaces, violet action/selection states, green healthy state, amber manual-release state, and visible focus styling.
- Image quality and asset fidelity: the source uses no photographic or decorative raster assets. Product icons use the existing Phosphor icon library; no emoji, custom SVG, CSS illustration, or placeholder image substitutes were added.
- Copy and content: new control-plane copy is coherent in Chinese and English. Persisted conversation content remains in its authored language. Missing traffic and evaluation values are stated truthfully.
- Interaction and accessibility: tested desktop page navigation, recent-chat routing, chat search and empty state, New chat creation, mobile recent-chat dialog, hash routes, locale switching, tabs, release dialog, disabled confirmation, manual reason entry, release success, production alias update, semantic labels, keyboard-capable controls, reduced motion, and mobile persistence of primary navigation.

## Comparison history

1. The first desktop comparison showed the A2UI card narrower and its comparison labels smaller than the source. The Agent conversation column changed from 672 to 752 pixels and comparison typography increased. The post-fix evidence is `/tmp/chat2ranker-agent-release-final-v2.png` and `/tmp/chat2ranker-agent-final-comparison.png`.
2. The first 390-pixel mobile capture used a horizontally scrolling five-column route table, which wrapped the status header awkwardly. The mobile table now keeps Route, Agent version, and Status in a three-column layout and hides the secondary revision and timestamp columns. The post-fix evidence is `/tmp/chat2ranker-deployment-mobile-final.png`.
3. The final browser console check after a clean service restart showed only Vite connection and React development messages. A transient hot-reload error occurred while the `TaskSidebar` export and its import were changed in separate edits; the subsequent App update and clean restart resolved it. The source and implementation were inspected together in the combined full-view and focused release-card comparisons listed above.
4. The sidebar follow-up compared the supplied ChatGPT information architecture and the Rank implementation in one canvas. The implementation deliberately keeps Rank's light visual system while matching the single-sidebar hierarchy, page actions above history, active-conversation treatment, account footer, and a functional search state. Mobile retains a compact bottom navigation and exposes recent chats from the top bar.

## Implementation checklist

- [x] Preserve Agent-first conversation and inline tool/A2UI progress.
- [x] Keep manual Deployment inspection and release available.
- [x] Compare current and candidate versions with a directional arrow.
- [x] Keep “Open Deployment page” inside the card.
- [x] Provide Chinese and English control-plane copy.
- [x] Use one text-labeled desktop sidebar with page navigation above recent chats; keep compact icon navigation on mobile.
- [x] Avoid fabricated traffic and evaluation metrics.
- [x] Verify desktop and mobile browser states with no console errors.

## Acceptance follow-up

The follow-up audit used the running product rather than cached captures. Accepted evidence is `/tmp/chat2ranker-acceptance-2026-09-09/05-workspace-after.png`, `/tmp/chat2ranker-acceptance-2026-09-09/14-workspace-mobile-final.png`, `/tmp/chat2ranker-acceptance-2026-09-09/10-deployment-desktop.png`, `/tmp/chat2ranker-acceptance-2026-09-09/09-deployment-mobile.png`, `/tmp/chat2ranker-acceptance-2026-09-09/11-release-dialog.png`, `/tmp/chat2ranker-acceptance-2026-09-09/12-deepseek-rendered.png`, and `/tmp/chat2ranker-acceptance-2026-09-09/13-final-workspace.png`.

1. Conversation entry and discovery: healthy. The duplicate Experiments and Workspace navigation items are removed; Experiment remains the persisted task and Run grouping behind conversations and inspectors. New chat is the primary action, recent items are text-first, and the header search action reveals filtering only when needed.
2. Conversation and A2UI actions: healthy. The conversation owns its scroll area, the composer remains visible, and release cards retain their current-to-candidate comparison and Deployment link on desktop and mobile.
3. Real Control Agent response: healthy after repair. A live DeepSeek response omitted one JSONL separator; the parser now separates adjacent top-level Rank Message objects and renders them as structured content instead of exposing JSON.
4. Deployment overview and release dialog: healthy. Desktop and 390 x 844 mobile states preserve legible routes, truthful empty traffic, policy values, labeled controls, and disabled confirmation until a reason is supplied.
5. Accessibility inspection: no screenshot-visible blocking issue. Hidden file inputs are removed from the accessibility tree, keyboard focus styling remains visible, labels and reading order are present, and reduced-motion CSS remains available. Screen-reader announcements, browser zoom above 200%, and automated color measurements were not established by screenshots and remain separate verification work.
6. Performance and persistence: healthy for this local slice. The production JavaScript bundle is 105.42 kB gzip, and SQLite query plans use covering composite indexes for request-window and active-status admission checks.

The live acceptance also completed a Control Agent turn and a public `deepseek-acceptance@production` Response through DeepSeek. The accepted Response remained pinned to one Agent version and ReleaseRevision. The temporary service home and credential material were deleted after the check.

final result: passed
