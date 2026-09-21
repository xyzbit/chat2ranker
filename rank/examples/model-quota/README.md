# Shared model quota example

English | [中文](README.zh.md)

This keyless example starts a packaged Execution API, two Worker processes and actual DSH against a local HTTP provider and an isolated PostgreSQL schema. It verifies shared connection RPM, independent TPM blocking, cancellation, replay, restart persistence and transient credential exclusion from request artifacts.

## Run

Build DSH with `pnpm run build:lib:host`, provide an expendable PostgreSQL URL through `EXECUTION_TEST_DATABASE_URL`, then run `pnpm run rank:test:model-quota` from the repository root. The test builds Go binaries with Go 1.26.0 and compares `snapshot.json`. No provider key is used. Child processes and private temporary files are removed; the printed `quota_<hex>` schema is retained for inspection. Remove only that printed schema with `DROP SCHEMA <printed_schema> CASCADE` when finished; never point the test at a production database.

## Configure shared allowances

Use PostgreSQL for this capability. Set `EXECUTION_MODEL_QUOTA_URL` to a runtime-reachable `/v1/model-permits` endpoint and supply a separate `EXECUTION_MODEL_QUOTA_KEY` of at least 32 characters to API and Workers. It signs invocation-only callback identities; do not send the service token to runtimes. `-model-quota-timeout` defaults to 5 seconds, and `-model-permit-validity` defaults to one second. Configure a group with `executiond -configure-model-quota quota.json` using the same database/schema as the service:

```json
{"id":"provider-account","connections":["connection-a","connection-b"],"requestsPerMinute":60,"tokensPerMinute":100000,"maxOutputTokens":512}
```

Use real verified connection IDs. Zero disables one rate dimension; at least one must remain positive. Connections sharing an upstream account must belong to the same group. Rate edits preserve live debits. Membership edits require drained queued, active and cleanup-pending work plus elapsed accounting windows. Output caps freeze at admission; configuration races return HTTP 409 and allow resubmission with the same idempotency key.

Permits serialize actual DSH stream attempts across Workers. Accounting lasts 60 seconds plus the permit's maximum dispatch interval. Token reservations are conservative estimates with positive usage reconciliation, not exact provider billing or a monetary cap. Missing usage retains the debit. Exhausted pre-start allowance is excluded from scaling demand, but the pre-start check does not reserve a first call: a running harness may wait while consuming its slot and fixed lifetime. Unsupported adapters and SQLite reject quota-bearing work.

For minikube remote-runtime and bounded DeepSeek verification, see the [deployment lab](../../deploy/k8s/README.md).
