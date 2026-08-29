import assert from "node:assert/strict";
import test from "node:test";
import { isManualModel, modelOptions } from "../src/model-connections.js";

test("merges official and discovered models without duplicates", () => {
  assert.deepEqual(modelOptions([{ id: "official-a", name: "Official A" }], ["official-a", "account-b"]), [
    { id: "official-a", name: "Official A", source: "official" },
    { id: "account-b", name: "account-b", source: "discovered" },
  ]);
});

test("recognizes a manually entered model id", () => {
  const options = modelOptions([{ id: "official-a", name: "Official A" }], []);
  assert.equal(isManualModel("official-a", options), false);
  assert.equal(isManualModel("preview-x", options), true);
});
