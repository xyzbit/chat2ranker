# Local Rank integration

English | [中文](README.zh.md)

This tutorial calls a published Rank Agent using the official OpenAI SDK from an independent Node.js client. The default example remembers `RANK-2048`, asks for it in a streamed second turn, retrieves the saved response, and asserts recall and the resolved Agent version. It makes two model calls and may incur provider charges.

## Prerequisites

Use Node.js 22.19+ and a running Rank build that implements `/v1/responses`, Deployments, and serving credentials. The evaluation-only build does not provide these endpoints. Publish a real-model Agent to a Deployment's `production` alias and verify its model connection in Rank. The example does not start or modify the Agent runtime or publish a release.

## Run locally

From this directory:

```sh
npm ci
RANK_MODEL=acceptance-mtu67jzo@production npm run verify:local
```

Replace the deployment name with your own. This local verification uses the loopback operator API to create a temporary key scoped to that Deployment with create/read permissions, runs the client, and revokes the key in `finally`. It never prints or saves the key. Accepted Response records remain in Rank. If the process is forcibly killed or revocation fails, revoke the credential named `Local Responses example` in Rank.

Successful output contains both replies, the resolved model and Response IDs, a `PASS` line, and confirmation that the temporary key was revoked. A failed request or assertion exits with code 1; an error is not a successful integration result.

## Connect your application

Create a serving key in Rank with access to the Deployment and `responses:create` / `responses:read` scopes. Create a local `.env` using [.env.example](.env.example) as the field reference:

```dotenv
RANK_BASE_URL=http://127.0.0.1:8787/v1
RANK_MODEL=your-deployment@production
RANK_API_KEY=your-rank-serving-key
```

Keep the key in your application's server environment. It is a Rank serving key, not a DeepSeek or other provider key. `.env` is ignored by Git.

```sh
npm start
npm start -- "Give me a one-sentence introduction."
```

Without a prompt, the client runs the two-turn example. With a prompt, it streams a new single-turn conversation. [client.mjs](client.mjs) contains the public API integration; [verify-local.mjs](verify-local.mjs) is an operator convenience for local verification and is not needed by an external application. The SDK has automatic retries disabled so failures do not silently submit additional work. The example uses only the declared Responses subset, not full OpenAI API compatibility.

## Troubleshooting

Connection refused means the Rank API is not listening at the configured address. `404` on the serving route means the build lacks Serving support; a resource `404` can also mean an incorrect deployment or insufficient scope. `401` means the serving key is missing, revoked, or invalid. `500` while creating a key is a Rank service or storage failure: inspect the service logs and restore its working directory and database before retrying. A health endpoint returning `200` alone does not prove that writes or model execution work.
