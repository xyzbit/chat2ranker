# Agent Note: Rank streaming control and contextual inspection

Status: implemented

English | [中文](2026-08-24-rank-streaming-control-and-inspector.zh.md)

## Problem

Rank's control conversation waited for a complete DeepSeek Harness turn before showing the user's message or any activity. Dataset and Agent references were plain text, while detailed dataset cases and run results expanded inside chat. Natural-language case additions also created unrelated dataset families because the control tools had no immutable append operation.

## Decision

The Rank UI inserts the user message immediately and starts one asynchronous Control Turn. The Control Host projects the persistent DSH Session Event stream into a small SSE vocabulary: turn lifecycle, safe reasoning status, tool lifecycle, and assistant text deltas. Raw reasoning text is never sent to the browser. The completed DSH transcript remains the durable conversation and is reconciled into Rank as before.

Composer mentions carry dataset or Agent version IDs separately from visible text. The Control Host adds those references to the model-visible user message and removes the reference block when projecting the user transcript. The composer uses mentions for unambiguous conversational selection and keeps only three compact shortcuts: run configuration, experiment performance, and case import.

Chat retains compact run summaries. A closed-by-default experiment workspace docks beside Chat on desktop and overlays it on narrow screens. Its Configuration tab presents the selected dataset and Agent versions; its Run History tab presents only the current experiment's runs. Dataset, Agent, run, and case actions open the workspace on the owning entity, and a width toggle gives case lists, logs, trials, and artifacts more room without creating a second product route. New Experiment and Experiment History remain global header actions rather than workspace navigation.

Run preparation and experiment performance are durable A2UI conversation events rather than permanent cards at the end of Chat. `rank_prepare_run` emits the confirmation snapshot only for a run intent. `rank_show_experiment_results` reads every Run and tested Agent version in the current experiment, then emits the aggregate chart only for a data, result, performance, or comparison intent. The composer shortcuts send those same intents. The Run History tab may keep the aggregate plot visible because it is a dedicated inspection surface. Each chart point is one run, pass rate is the vertical axis, and the user switches the horizontal axis between known cost and total latency; when no complete cost exists, latency is selected initially. Runs without the selected metric remain counted but are omitted from the plot. Selecting a point opens that immutable run in the experiment workspace.

Agent Tool and Skill references use searchable multi-select fields populated from existing Agent versions and a small seed catalog. A custom identifier can still be entered explicitly. The form states that the selected Runner Adapter must provide each Tool and the execution environment must install each Skill. Direct submission creates an immutable Agent version; the conversation entry projects the same configuration into `rank_create_agent` or `rank_create_agent_version`, and the latter copies omitted fields from its base version.

The experiment workspace exposes case-level add/edit actions in dataset detail and an edit action in Agent detail. These forms never update the selected asset in place: saving creates the next version in the same family and immediately selects it for the experiment, while historical runs keep their frozen version IDs.

Dataset and Agent areas in setup and run-confirmation cards keep the main surface as a detail action and add one compact switch icon that opens the matching asset picker. Run costs remain strict about completeness: if any Trial has unknown cost, the known aggregate is rounded down and displayed as a lower bound such as `≥$0.0599` and labelled `known cost`; cost comparison charts plot these lower bounds as hollow dashed points instead of hiding the Run. Run summaries carry the same `costKnown` fact as full Run records.

Final control-agent replies use Rank Message JSONL with a small set of summary, paragraph, list, facts, note, and code blocks. The browser validates model output at the wire edge and renders these blocks with product-owned typography; an incomplete streaming line stays hidden until it forms a complete object. Existing Markdown messages use a safe legacy renderer instead of exposing syntax markers. Copying either representation produces Markdown, first through the asynchronous Clipboard API and then through a synchronous browser fallback; the button reports success or failure at the interaction point. The durable DSH transcript and Rank message content keep the original model output.

`rank_add_dataset_cases` requires a base dataset version and additions. Rank copies the base cases, creates the next immutable version in the same repository transaction, and selects that version for the experiment. The base version is never updated.

## Alternatives considered

**Synthetic frontend progress.** Rejected because timer-based phrases can disagree with the harness. The projection uses actual DSH tool and message events.

**Expose raw chain-of-thought.** Rejected because the product only needs observable progress. A generic reasoning status plus tool lifecycle provides that without leaking hidden model reasoning.

**Put all detail in chat.** Rejected because case lists, repeated trials, logs, and artifacts obscure the experiment decisions that the conversation should preserve.

**Open detail in a modal inspector.** Rejected because a modal hides the conversation that explains the selected version or run. The docked workspace preserves that context while remaining optional.

**Free-form comma-separated capability fields.** Rejected because identifiers were hard to discover and easy to mistype. Searchable multi-select keeps known choices visible while preserving an explicit custom-ID path.

**Mutate the selected dataset.** Rejected because runs freeze version IDs and must remain reproducible. Appending always creates a new version.

**Send every manual edit through chat.** Rejected because correcting one case or Agent field is faster and clearer in the owning detail view. Chat remains available for intent-driven and bulk changes.

**Hide a partial aggregate as unavailable.** Rejected because it discards useful accounting that is already durable. Presenting the known amount as a lower bound preserves both utility and honesty.

**Render model Markdown directly.** Rejected as the primary format because arbitrary heading, table, and list choices make message density unpredictable. Markdown remains an import and copy format rather than the visual protocol.

## Consequences

Sending feels immediate, control work is visible, and `@` references resolve stable versions. The in-memory Control Turn SSE survives late subscribers for five minutes but does not survive a Control Host restart; the durable DSH transcript still recovers the completed conversation. The experiment workspace owns read-heavy detail and manual asset selection while Chat remains the primary flow. Desktop width is shared when the workspace is open; small screens intentionally give the workspace the full viewport. Charts compare completed runs without creating a dashboard route. Adding cases or changing Agent capabilities costs one new immutable version rather than changing historical runs.
