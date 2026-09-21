# Local Integration Feedback

English | [中文](integration-feedback.zh.md)

On 2026-09-13, a first-user walkthrough created a real-model Agent, published `demo-integration`, created a scoped key in the Integration drawer and ran the [headlines and chat examples](../examples/responses-client/README.md). This record separates verified capabilities from remaining usability gaps.

## Walkthrough findings addressed

| Issue | Reproduction and impact | Recommendation |
| --- | --- | --- |
| Agent creation leaves the asset page | New Agent > Create and Select opens the existing Rank conversation with its old experiment title; finding the new Agent requires returning to the list. | Open the new Agent after asset-page creation; retain chat context only for creation initiated there. |
| Misleading runtime warning | DSH initially reports a missing `DEEPSEEK_API_KEY`, yet runs after selecting a verified connection. | Distinguish missing installation from an unselected credential source, using the resolved configuration. |
| Empty first-release table | Four empty metric rows appear without development evaluation or an earlier version; no regression dataset disables publication. | Prioritize version, model and regression choice, explaining blockers in empty states. |
| No direct Response ID lookup | Code has the ID, but the list lacks ID search and detail selection is absent from the URL, so reload loses it. | Add ID search and addressable detail links for examples. |

The asset creation path opens the new Agent, the runtime distinguishes installation from connection selection, first-release empty states explain missing evidence and regression data, and invocation records support ID search and refreshable detail URLs.

## Documentation and remaining gaps

The old CLI tutorial used a fixed test endpoint and `@production`, diverging from the endpoint-name-as-model introduction. The example README and environment template use endpoint names and explain the 4173 frontend proxy versus 8787 API address. Historical alias details in the reference still need separation from introductory guidance.

The platform quickstart includes the filename, Node requirement, ESM invocation and expected output. The complete examples topic links runnable headlines and chat applications with commands and acceptance checks.

The streaming snippet requires completion and rejects truncation. The full example covers cancellation and continuation state and documents the new-conversation requirement after cancellation. Copy to Agent supplies a directly readable Markdown API reference. Integration and documentation offer standalone ZIPs with a bilingual tutorial, dependencies and endpoint-prefilled environment templates. No repository checkout is required, and keys remain blank in downloads.

Fresh-directory acceptance exposed a cancellation race: when a queued Response began running during cancellation, the API returned the running state without cancelling it. Cancellation retries that conflict and stops the acquired execution; an already terminal result remains unchanged. Error guidance distinguishes rejected requests without a Response ID from accepted execution failures with invocation records.

[Invocation feedback](response-feedback.md) supports diagnosis and reviewed dataset capture. Multi-turn context, additional instructions and tool results require manual consolidation into standalone text; automatic replay needs a richer case format. A verified model connection can still reference an unavailable model, so evaluation errors must identify Judge failures separately from candidate quality.

External-tool support does not imply automatic current-news retrieval. This example collects live data in the application before one Rank call. Agent-selected functions require application execution and result submission, usually involving several Responses requests; tutorials should establish the chosen approach first.

## Content acceptance

The invocation walkthrough captures a redacted chat input, evaluates the frozen Agent against dataset v1 and obtains 1/1 valid passing Trial. Appending a second reviewed continuation case creates v2 with two cases while the completed Run retains v1 and one case. This verifies the feedback workflow, not general Agent quality. Cost is displayed as unavailable because pricing is missing.

The real model retains the name, technology-news preference and ten-minute allowance across two turns. Initial headlines inferred controversy from comment counts, which title data cannot establish. Example instructions restrict output to title translation and summary without those inferences. Sources and collection timestamps remain available separately for verification.

The example covers HN submissions within 24 hours, not domestic trending searches, multi-source fact checking or full-article research. Source failures stop execution instead of substituting model memory. `npm run verify:demos` writes real outputs, timing and version information to local artifacts; content varies with news and model behavior.
