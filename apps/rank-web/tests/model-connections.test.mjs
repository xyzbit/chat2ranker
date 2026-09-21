import assert from "node:assert/strict";
import test from "node:test";
import { modelOptions } from "../src/model-connections.js";

test("merges official and discovered models without duplicates", () => {
  assert.deepEqual(modelOptions([{ id: "official-a", name: "Official A" }], ["official-a", "account-b"]), [
    { id: "official-a", name: "Official A", source: "official" },
    { id: "account-b", name: "account-b", source: "discovered" },
  ]);
});
