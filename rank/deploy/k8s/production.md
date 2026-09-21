# First production deployment

English | [中文](production.zh.md)

This reference describes the smallest supported production topology and the decisions an operator must supply. The minikube manifests are an executable lab, not a ready-made public deployment. Physical multi-host capacity verification belongs to the operator.

## Placement and scaling

Start with one Rank API, one control host, one Execution API and a bounded Worker pool. Keep PostgreSQL and object storage durable outside disposable application containers. Rank's current dispatch ownership is single-owner; do not scale Rank replicas without adding and verifying ownership coordination. A Worker owns several configured execution slots, not one permanent Agent or conversation. Each remote invocation receives a short-lived Sandbox; database records and S3 artifacts survive its deletion.

KEDA consumes authenticated Execution capacity projections. Scale on eligible online/evaluation demand, with a warm minimum and an explicit maximum constrained by model and Sandbox capacity. The lab uses one slot per queue per Worker and 1–3 Worker replicas. Adding Worker Pods does not add capacity to the lab's fixed Docker Sandbox host. Production must provide measured capacity on each actual runtime host or adopt a scheduler-backed Sandbox provider before claiming horizontal runtime growth.

Keep API/Worker limits and runtime plans compatible. Freeze runtime images by digest and deploy the API, Worker protocol and Sandbox images together. Disable autoscaling and drain producers before a schema upgrade, apply explicit migrations using migration-only credentials, then restore runtime roles and traffic. Back up PostgreSQL and object storage together with their retention and restore procedures; a backup without a tested restore is not an availability guarantee.

## Access and communication

Publish the browser and public Responses API through a TLS gateway with an explicit public origin. Preserve SSE and choose gateway idle/read limits greater than the configured response lifetime. Prefer routing the browser's API and control requests through its origin. If hosting the frontend separately, configure and verify the exact trusted origins; do not use wildcard credentialed CORS.

Keep PostgreSQL, object-storage administration, Execution administration, control commands and Sandbox lifecycle APIs on authenticated private networks. Runtime model-permit traffic uses a dedicated path, its own invocation identity and bounded requests. The lab's port 30091 forwards only that path; production must keep it private and use TLS across hosts. Never expose the lab NodePort or plain HTTP management addresses to the Internet. Do not inject Execution service tokens, signing keys or database credentials into Sandbox processes.

Store operator credentials, model keys and internal keys in the deployment's secret manager. Operator revocation reloads without restarting Rank. Rotate shared service and quota-signing keys in a coordinated, drained maintenance window unless a separate overlap mechanism is implemented. Give applications DML-only database roles; migration owners run as short-lived deployment jobs. The current workspace is a trusted shared administration model, not a completed enterprise identity platform.

## Evidence and retention

Committed conversation and execution rows remain authoritative even when tracing is unavailable. Objects use execution IDs and content digests; API reads require a committed database reference. Successful uploads can be orphaned if the Worker disappears before reference commit. Keep an operator-selected retention window longer than the maximum execution, rescue and cleanup interval. After an execution is terminal and cleanup is confirmed, compare its object prefix with recorded artifact references before selecting unreferenced objects for deletion. Preserve referenced evidence and active or unresolved attempts. Automatic orphan collection is not implemented.

Observe accepted/completed/failed/cancelled counts, queue delay, first output latency, quota/resource blocking and cleanup backlog separately. Report actual model usage and missing pricing explicitly. Shared TPM is an estimate adjusted by positive provider usage; it is not exact billing or a hard spend limit. Quota waiting inside DSH still occupies its runtime. The [acceptance reports](../reports/2026-09-15-management-access.md) identify concrete lab evidence; local measurements do not establish a production throughput SLO.

## Before admitting traffic

Verify TLS and browser origin behavior, credentials and rotation, persisted-volume recovery, the configured Sandbox isolation and egress rules, bounded queue/runtime/cleanup deadlines, and model-account quota membership. Replay the [lab checks](README.md) on the chosen environment. Measure physical host capacity with representative Agent tools and models; then set Worker maximums and overload behavior from that evidence. The current limits do not prove arbitrary hostile-code isolation, platform high availability, or recovery from loss of both the database and its backups.

Browser login requires frontend, API and control routes under the same public origin. Configure that exact HTTPS origin on both Rank and control-host to enforce mutation origins and Secure session cookies. The current process-local session store follows the single Rank replica; maintenance restarts require operator login again. See [management access](../../backend/README.md#management-access).
