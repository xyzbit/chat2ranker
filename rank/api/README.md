# Rank protocol

English | [中文](README.zh.md)

This directory owns Rank product and Control DSH API documentation. Generic execution payloads live in `execution/backend/contract` so they do not depend on Rank business types.

The implemented HTTP API exposes:

- experiment, dataset version, agent version, run, and run-item identifiers;
- immutable dataset and Agent version creation and selection;
- persistent Control DSH commands with idempotent command identifiers;
- create, start, cancel, and query operations for Rank runs, including a 1–20 `trialCount` (default 5);
- `GET /api/runs/{id}/events` SSE with replay from a monotonic cursor;
- frozen Evaluator snapshots, structured Trial/criterion results, reliability and pass^3 aggregates, separated failure counts and costs;
- failed-case results and authorized artifact reads.

The Go HTTP handlers own Rank transport DTOs. The browser client owns only the corresponding JSON calls and SSE subscription. Generic create, watch, cancel, query, event, and terminal-result contracts remain in `execution/backend/contract` and its Go client.

## Evaluator payload

`DatasetVersion.rubric` accepts an Evaluator document. Rank normalizes it, assigns a version identity, and freezes it into every Run:

```json
{
  "name": "Citation quality",
  "deterministic": [
    { "id": "json", "name": "Valid JSON", "operator": "json_valid", "required": true }
  ],
  "rubric": [
    { "id": "grounded", "name": "Grounded claims", "description": "Every factual claim has traceable evidence", "weight": 2, "threshold": 0.8, "critical": true }
  ],
  "judge": { "harness": "dsh", "model": "deepseek-chat" },
  "passPolicy": { "rubricThreshold": 0.75 }
}
```

Deterministic operators are `equals`, `contains`, `not_contains`, `regex`, and `json_valid`. A case may also declare the convenience expectations `exactOutput`, `outputContains`, `outputRegex`, or `jsonValid`; Rank converts them into required deterministic criteria. Criteria with `required: true` gate Judge execution. Rubric pass/fail is derived from the frozen score thresholds, and critical failures override the weighted aggregate.
