# Rank API rolling example

English | [中文](README.zh.md)

This keyless example runs real DSH/Execution requests through the local Kubernetes gateway while replacing Rank HTTP Pods. It requires the [complete remote platform lab](../../deploy/k8s/README.md), one HTTP-only API replica and one separate dispatcher. It never calls a paid model provider.

```sh
node rank/examples/api-rolling/verify.mjs
```

A deterministic model passes one regression case before publication. A 25-second model response exercises drain completion; a 45-second response exceeds the API's 30-second HTTP shutdown budget. Continuous history and cookie-authenticated identity reads must return 200 throughout both rollouts. The long stream must end without a success terminal event, while its original Response later completes at one Execution attempt and supports durable SSE replay. Login survives replacement; logout revokes the shared session. The dispatcher Pod remains unchanged.

The script compares these outcomes with [snapshot.json](snapshot.json), retains IDs and timings under the private lab state directory, revokes its public key and removes its model container and port-forwards. It does not change real model credentials. Fixtures and publication records remain identifiable by their `rolling-` prefix. This verifies schema-compatible API replacement on one host, not control-host replacement, migrations or node-failure high availability.
