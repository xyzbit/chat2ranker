# Run a Rank Example

English | [中文](README.zh.md)

This tutorial runs a downloaded example without a Rank repository checkout. Daily headlines collect current source data and create one Response. Chat uses streaming and continues from the latest completed Response ID. Both call the published endpoint through the official OpenAI SDK; the API key stays in the local Node.js server.

## 1. Prepare

Install Node.js 22.19 or newer and keep Rank running at the address in `.env.example`. The endpoint must have a published Agent using an available real model. These text examples need no local tools or Skills. In the endpoint Integration drawer, create an API key for that endpoint with create, read and cancel permissions. Keep its original value locally; Rank cannot recover it later.

## 2. Start

Extract the ZIP and open a terminal in its `rank-news` or `rank-chat` folder. Run:

```sh
npm ci
cp .env.example .env
```

On Windows PowerShell use `Copy-Item .env.example .env` instead of `cp`. Open `.env` in your editor and fill `RANK_API_KEY` with the Rank serving key, not the underlying model provider key. `RANK_MODEL` is the endpoint name. The download preconfigures its address and name; edit them if the platform moved. Keep `.env` out of repositories and browser code. Then run:

```sh
npm start
```

Open the URL printed by the terminal. Headlines default to port 4181 and chat to 4182. If a port is occupied, change `DEMO_PORT` in `.env`. Restart after editing configuration. The header links to documentation on the configured `RANK_PLATFORM_URL`; request details link to the same platform's invocation records.

## 3. Try the result

**Daily headlines:** choose Generate. The app retrieves HN submissions from the last 24 hours, selects up to ten titles by community score, then sends exactly one Rank request for five Chinese summaries with numbered sources. A source failure stops before the model call. This is community activity summarized from titles, not global trends or full-article verification. `npm run news` runs the same task in the terminal; it does not schedule daily work.

**Chat:** send “My name is Xiaolin, I like technology news and have 10 minutes daily,” then ask “What is my name and how much time do I have?” Expect streamed text and recall of the earlier facts. Each browser visitor has a server-held Response ID. Tabs share that visitor's conversation; separate visitors are isolated. Reload retains the running process's transcript. Stop calls Rank cancellation, after which New Conversation is required. Demo restart clears sessions, while Rank keeps invocation records.

## 4. Verify

```sh
npm test
npm run verify:demos
```

The first command runs keyless HTTP tests and a transcript snapshot. The second verifies both workflows against the configured real endpoint, making four Rank requests and cancelling the last. It can incur model charges and requires HN access. Missing configuration and failures exit nonzero. Evidence contains no keys and is written to `output/integration-demos-live.json` inside this folder.

## Troubleshooting reference

| Symptom | Action |
| --- | --- |
| Connection refused | Start Rank, check `RANK_BASE_URL`, and restart the demo after edits. |
| 401 | Replace an invalid or revoked Rank key; do not use a provider key. |
| 403 / 404 | Check endpoint name and endpoint/operation permissions. Invisible resources can return 404. |
| 409 | Publish the endpoint. Continue only from the latest completed Response, with one running turn per conversation. |
| 429 | Check endpoint rate/concurrency limits and wait. SDK automatic retries are disabled. |
| HTTP 200, failed result | Check `status` or the terminal SSE event, retain the Response ID, and open its invocation record. |
| Interrupted stream | Reload to see saved state. Disconnecting does not cancel Rank execution; do not blindly resend. |
| Source failure | Check access to `hn.algolia.com`. No Rank request or Response ID exists yet. |

For an accepted request, expand Request Details and follow Invocation Records to inspect the frozen Agent version, input, output and failure. Rejected requests such as invalid keys usually have no Response ID or invocation record: retain the HTTP status and error code. Do not share keys in diagnostics. A stream succeeds only on `response.completed`; `response.failed`, `response.cancelled` and missing completion are not successful output.

The platform documentation covers additional instructions, external functions, SSE recovery and chat integration. These demos are local teaching applications: sessions expire after 24 hours or restart, keys remain server-side, and the server binds to loopback. Production use requires authentication, persistent sessions and access control. External function execution is outside these text demos; follow the external-tools guide for the function-call/result loop.
