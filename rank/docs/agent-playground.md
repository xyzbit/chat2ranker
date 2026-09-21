# Agent playground

English | [中文](agent-playground.zh.md)

## Conversations

Playground conversations invoke an immutable Agent version with its model connection, prompt, tools, and Skills through the same Execution Service as external Responses. They do not create a Dataset, evaluation Run, public API key, or implicit Deployment. Deployment previews resolve an alias at the first send and retain its version and release revision across later promotions.

Agent details open a conversation panel alongside the configuration on wide screens and across the screen on mobile. The selected version restores its newest direct conversation; the new-conversation action starts fresh. History is scoped to the Agent family and identifies each version. Closing the panel preserves its draft and ongoing response. Deployment routes open the same panel in their page. Conversation URLs support reloads; existing `#/trials?conversation=...` links open the Agent panel.

Rank control chat remains an experiment workspace. Its `rank_open_trial` tool persists an A2UI handoff carrying an exact version and optional draft. Opening the handoff does not execute the Agent.

The shared conversation dock has Rank and Trial tabs. Switching tabs preserves each draft, history, and active response independently; it never transfers trial messages into Rank. Rank requests sent from Agent details include the viewed version as a structured mention unless the user explicitly selected another Agent. Evaluation links create a separate Rank experiment, select the viewed version and its development default, and prefill an unsent mention and evaluation request.

## Operator API

These routes use the existing local operator API access model, not public serving credentials. The configured Rank tenant owns trials. A public serving key cannot read, cancel, or continue trial Responses.

| Route | Behavior |
| --- | --- |
| `GET /api/trials?before=<conversation-id>` | Newest-created conversations, 30 per page; pass the last ID for older history. |
| `GET /api/trials/{id}` | Frozen Agent, conversation, and ordered user inputs and Responses. |
| `POST /api/trial-responses` | Start with `agentVersionId` or `deploymentReference`, plus text `input`. Continue with `conversationId`, `previousResponseId`, and `input`. |
| `GET /api/trial-responses/{id}/events` | Replayable SSE; `Last-Event-ID` or `starting_after` selects the event cursor. |
| `POST /api/trial-responses/{id}/cancel` | Cancel accepted work without deleting its history. |

History accepts optional `agentFamilyId`, `agentVersionId`, and `directOnly=true` filters, applied before pagination. Direct-only history excludes deployed previews.

Creation accepts `Idempotency-Key`. Repeating identical input with the same key returns the accepted Response; changed input conflicts. Continuation checks the conversation head transactionally and rejects concurrent sends. Failed or cancelled turns remain visible but are excluded from subsequent model context; completed user/assistant exchanges are replayed using the existing serving history mechanism. Restart resumes active Responses from frozen Execution requests and transactionally persisted progress cursors. Cancellation records intent first; the trial stays active with `cancelRequested: true` until Execution confirms a terminal outcome.

Direct previews use `rankd -trial-timeout` (2 minutes), `-trial-requests-per-minute` (60), and `-trial-concurrency` (4). Direct previews share these admission limits within the configured tenant. Deployed previews inherit their Deployment execution policy but never send production Webhooks or enter external request history. Model credentials remain in Execution Service.

## Supported interaction

The browser supports text conversations, adapter progress, cancellation, retry, copying, configuration inspection, and execution details. Costs remain unknown without provider accounting or configured pricing. Progress is displayed when the adapter emits it; some CLIs return the assistant answer only at completion. The playground inherits harness tool permissions and does not supply caller-side function implementations or a new interactive approval protocol. Those business integrations require their own caller, as with the public API.
