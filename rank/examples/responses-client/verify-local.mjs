import { runExample } from "./client.mjs";

const baseURL = process.env.RANK_BASE_URL || "http://127.0.0.1:8787/v1";
const url = new URL(baseURL);
if (!["127.0.0.1", "localhost", "[::1]"].includes(url.hostname) || url.protocol !== "http:") {
  throw new Error("Local provisioning requires an HTTP loopback endpoint.");
}
const model = process.env.RANK_MODEL || "acceptance-mtu67jzo@production";
async function operator(path, body) {
  const response = await fetch(`${url.origin}${path}`, {
    method: body === undefined ? "GET" : "POST",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(15_000),
    redirect: "error",
  });
  const value = await response.json();
  if (!response.ok) throw new Error(`Operator ${path}: HTTP ${response.status} (${value.error?.code || "unknown"})`);
  return value;
}

let credential;
let tenantId;
try {
  const bootstrap = await operator("/api/bootstrap");
  tenantId = bootstrap.tenantId;
  const deployments = await operator(`/api/deployments?tenantId=${encodeURIComponent(tenantId)}`);
  const deployment = deployments.find(item => item.name === model.split("@")[0]);
  if (!deployment) throw new Error(`Publish a deployment and set RANK_MODEL; ${model} was not found.`);
  credential = await operator("/api/serving-credentials", {
    tenantId, name: "Local Responses example", deploymentIds: [deployment.id],
    scopes: ["responses:create", "responses:read"],
  });
  await runExample({ baseURL, apiKey: credential.apiKey, model });
  console.log("PASS: synchronous call, SSE, continuation, version pinning, and retrieval");
} catch (error) {
  console.error(`Failed: ${error.message}`);
  process.exitCode = 1;
} finally {
  if (credential) {
    try {
      await operator(`/api/serving-credentials/${credential.id}/revoke?tenantId=${encodeURIComponent(tenantId)}`, {});
      console.log("Temporary serving key revoked.");
    } catch {
      console.error("Temporary key revocation failed; revoke 'Local Responses example' in Rank credentials.");
      process.exitCode = 1;
    }
  }
}
