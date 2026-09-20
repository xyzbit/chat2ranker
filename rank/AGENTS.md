# AGENTS.md — AgentKiln product

These rules apply to `rank/`, `execution/`, `apps/rank-web/`, and product launch, build, and release scripts. They supplement the [repository instructions](../AGENTS.md).

- Treat chat A2UI as durable conversation events, never as permanent cards appended to the page tail. Show run preparation only when the user asks to run, and show experiment performance only when the user asks for experiment data/results or uses the matching composer shortcut.
- Interpret “查看实验数据/表现/结果” as the current experiment's overall comparison across all Run and Agent versions; the experiment workspace may keep that aggregate view persistently available for detailed inspection.
- Keep one Run bound to one immutable Agent version. A multi-Agent comparison creates one RunGroup and fans out into one child Run per selected Agent version; single-Agent selection remains the default UI.
- Keep model credentials in the Execution Service credential store. Rank persists only a verified model-connection ID in Agent and Run snapshots; worker requests receive credentials transiently and must redact them from events, logs, and artifacts.
- Resolve cost from provider-reported actual cost, then a connection-local model price, then the built-in provider catalog. Never invent cost when all three are absent; label it as missing pricing configuration.
- In the run-confirmation card, one multi-select Agent control handles both replacement and comparison. Render selected Agent versions as a compact scrollable list with hidden scrollbars and an overflow fade; list rows open that exact version's inspector.
- Preserve run-draft Agent selections when the multi-select closes and when its confirmation card is reopened. Core dataset, Agent, rubric, tool, Skill, model-reference, and repeat-policy configuration must also be expressible through Rank conversation and rendered as reviewable A2UI; API keys and other secrets stay in deterministic secure forms and never enter chat.
- Minimize configuration effort: when all required information is present in chat, create directly; when only one or two values are missing, ask only for them in chat. Use structured UI for browsing candidates, reviewing or editing many fields, bulk operations, and secrets. User-facing handoffs must use natural language and never paste internal IDs or JSON into the composer.
- Reuse text after the active `@` as the mention-picker search query; filter datasets and Agents immediately and keep the picker keyboard navigable instead of adding a separate search field.
- Keep DSH `headless` as an internal evaluation default recorded in Agent and Run snapshots; do not expose Preset in ordinary forms or conversation.
- Every Run card, history row, and Run inspector identifies the frozen dataset version, case count, and Agent version so later experiment changes cannot make results ambiguous.
- Rank control-agent replies use line-delimited Rank Message JSON objects, and the frontend owns their visual rendering. Keep legacy Markdown as a display fallback for existing history and export structured replies as Markdown only when the user copies them.
