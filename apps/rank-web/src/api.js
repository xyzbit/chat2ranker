async function request(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: { "content-type": "application/json", ...options.headers },
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(payload.error?.message || `请求失败 (${response.status})`);
    error.code = payload.error?.code;
    error.status = response.status;
    throw error;
  }
  return payload;
}

function newIdempotencyKey() {
  return typeof globalThis.crypto?.randomUUID === "function"
    ? globalThis.crypto.randomUUID()
    : `${Date.now()}-${Math.random()}`;
}

export const rankApi = {
  bootstrap: () => request("/api/bootstrap"),
	setSystemModel: (role, input) => request(`/api/system-models/${encodeURIComponent(role)}`, { method: "PUT", body: JSON.stringify(input) }),
  createExperiment: (input = {}) => request("/api/experiments", { method: "POST", body: JSON.stringify(input) }),
  getExperiment: (id) => request(`/api/experiments/${encodeURIComponent(id)}`),
  updateExperiment: (id, patch) => request(`/api/experiments/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(patch) }),
  sendMessage: (id, content, mentions = []) => request(`/control/v1/experiments/${encodeURIComponent(id)}/messages`, { method: "POST", body: JSON.stringify({ content, mentions }) }),
  controlTurnEventsURL: (experimentId, turnId, after = 0) => `/control/v1/experiments/${encodeURIComponent(experimentId)}/turns/${encodeURIComponent(turnId)}?after=${after}`,
  executeCommand: (id, action, payload = {}, idempotencyKey = newIdempotencyKey()) => request(`/api/experiments/${encodeURIComponent(id)}/commands`, {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey, "X-Rank-Action-Token": action.token },
    body: JSON.stringify({ type: action.command, actionToken: action.token, payload, idempotencyKey }),
  }),
  createDataset: (input) => request("/api/datasets", { method: "POST", body: JSON.stringify(input) }),
  createDatasetVersion: (familyId, input) => request(`/api/dataset-families/${encodeURIComponent(familyId)}/versions`, { method: "POST", body: JSON.stringify(input) }),
  createAgent: (input) => request("/api/agents", { method: "POST", body: JSON.stringify(input) }),
  createAgentVersion: (familyId, input) => request(`/api/agent-families/${encodeURIComponent(familyId)}/versions`, { method: "POST", body: JSON.stringify(input) }),
  createModelConnection: (input) => request("/api/model-connections", { method: "POST", body: JSON.stringify(input) }),
  updateModelConnection: (id, input) => request(`/api/model-connections/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(input) }),
  verifyModelConnection: (id) => request(`/api/model-connections/${encodeURIComponent(id)}/verify`, { method: "POST", body: "{}" }),
  deleteModelConnection: (id) => request(`/api/model-connections/${encodeURIComponent(id)}`, { method: "DELETE" }),
  startRun: (id, trialCount = 5, agentVersionIds = [], idempotencyKey = newIdempotencyKey()) => request(`/api/experiments/${encodeURIComponent(id)}/runs`, {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ idempotencyKey, trialCount, agentVersionIds }),
  }),
  getRun: (id) => request(`/api/runs/${encodeURIComponent(id)}`),
  runEventsURL: (id, after = 0) => `/api/runs/${encodeURIComponent(id)}/events?after=${encodeURIComponent(after)}`,
  getArtifact: (runId, caseId, path) => request(`/api/runs/${encodeURIComponent(runId)}/artifacts?caseId=${encodeURIComponent(caseId)}&path=${encodeURIComponent(path)}`),
  cancelRun: (id) => request(`/api/runs/${encodeURIComponent(id)}/cancel`, { method: "POST", body: "{}" }),
};
