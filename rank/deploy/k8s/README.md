# Local Execution Kubernetes experiment

English | [中文](README.zh.md)

The first part of this tutorial deploys the trusted Execution slice: PostgreSQL, a SeaweedFS S3 service, one Execution API and two worker Pods. The image runs the [deterministic CLI example](../../examples/execution-lifecycle/README.md); it does not contain DSH, Rank, the web gateway or a production sandbox. It tests process distribution and retained evidence before platform integration.

To exercise the product end to end, bootstrap the base services below, then follow [the complete platform experiment](README.md#complete-platform-experiment) for the news and chat clients. Continue with [remote Sandbox integration](README.md#remote-sandbox-experiment) and [bounded autoscaling](README.md#bounded-autoscaling-experiment) when validating runtime isolation and worker capacity. An installed complete platform is upgraded with `platform-lab.py up`, retaining `--sandbox` if selected; do not downgrade it with `lab.py up`.

## Prepare and build

Use Docker, Go 1.26.0, Python 3.12 or later, kubectl and minikube. Start Docker with enough memory for the proposed 4-CPU/4-GiB cluster. From the repository root:

```sh
mkdir -p "$HOME/.local/state/agentkiln/rank-k8s"
KUBECONFIG="$HOME/.local/state/agentkiln/rank-k8s/config" minikube start -p rank-dev --driver=docker --cpus=4 --memory=4096
python3.12 rank/deploy/k8s/lab.py build
```

The build cross-compiles native Linux binaries for the host architecture, downloads SeaweedFS v4.47 with its release checksum and builds content-tagged fixture images. The Node base and PostgreSQL dependency are pinned by digest. PostgreSQL receives an owned lab tag before containerd import because digest-only Docker archives do not preserve a runnable repository tag. Application Pods use imported images with `imagePullPolicy: Never`. On a nonmatching build host, pass `--arch arm64` or `--arch amd64`. Build outputs and image identities stay under the private lab state directory, outside Git.

## Deploy and inspect

```sh
python3.12 rank/deploy/k8s/lab.py up
python3.12 rank/deploy/k8s/lab.py status
kubectl --kubeconfig="$HOME/.local/state/agentkiln/rank-k8s/config" --context=rank-dev -n rank-dev port-forward --address=127.0.0.1 service/execution-api 18790:8790
```

The installer waits for a ready node before importing images from the local Docker daemon. If the network plugin cannot download its image, resolve cluster readiness first; increasing the application rollout timeout does not repair it. The installer targets only the dedicated `rank-dev` context and namespace. It preserves generated database, S3 and encryption credentials in a mode-0600 local file, imports the images, starts persistent dependencies, runs an explicit migration Job, then starts the API and workers. Wait commands are bounded; a dependency or migration failure stops installation. Inspect that resource before retrying. A failed migration Job requires explicit operator handling; applying the same immutable Job does not reset its failure.

The forwarded Execution API requires the generated service token from the private `execution-lab.json` state file. Use it through a local environment or a client that reads that file; do not paste it into chat or a command argument. Queue limits, timeouts, resource requests and worker slots live in [the manifests](base/execution.yaml). Rank and Execution use separate restricted runtime roles and owner credentials reserved for migration Jobs; see the [database privilege decision](../../../.agents/notes/implemented/architecture/2026-09-15-rank-database-permissions.md). The object-service readiness check confirms a listening socket; Execution independently checks its bucket at startup.

The two worker Pods share neither workspace nor artifact staging. Database and object PVCs survive worker replacement. This single-node lab cannot establish physical-host throughput or storage HA. Whole-runtime isolation and physical-host scaling remain separate acceptance requirements in the [production design](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md).

## Run the acceptance experiment

After deployment, run the following command. It uses a temporary loopback port-forward and reads the private service token directly from the lab state file.

```sh
python3.12 -B rank/deploy/k8s/verify.py
```

The experiment scales only the dedicated lab workers to zero, one and two replicas, replaces the API/workers and persistent dependency Pods, and force-removes one worker during a trusted fixture invocation. Run it on an idle lab. It retains requests, events and objects for inspection and restores two worker replicas on exit. A JSON report under the external lab state directory records image identities, request IDs, replay and persistence checks, observed owners, elapsed batch time and queue latency. Comparing one and two workers measures this fixture on this laptop; it is not a production capacity target.

After stopping and starting only the `rank-dev` profile, verify the retained evidence without submitting new work. Replace the report path with the successful full-run report printed above:

```sh
python3.12 -B rank/deploy/k8s/verify.py --retained-report /path/to/passed-report.json
```

Dependency startup may restart Execution containers before they become ready. This read-only check waits for the API rollout and verifies the same completed invocation, SSE history, artifact digest and non-replayed unknown outcome. It does not measure restart availability.

## Complete platform experiment

The complete lab adds one Rank API, one persistent control host, a static Nginx gateway and a deterministic model-protocol service. It replaces the Execution fixture image with the real DSH runtime while retaining PostgreSQL, River and S3. The default backend is a trusted shared-workspace experiment; the optional remote backend below adds whole-runtime restrictions. Neither establishes production team isolation. Browsers use the account/password login page and an HttpOnly session cookie; API clients can use a named operator token through Basic or Bearer authentication; public `/v1` calls use endpoint API keys. Rank validates operator identities using the projected `rank-management` digest file, and control-host checks Rank before accepting public conversation requests. Internal control commands are not forwarded by the gateway. Bind port-forwards only to loopback.

Build the host runtime and web app, materialize the deployment closure, then build and install platform images. Use a new staging directory for each runtime snapshot. The installer requires the Execution dependencies from the earlier `lab.py up` step.

```sh
pnpm run build:lib:host
pnpm --filter @xyzbit/agentkiln-web run build
python3.12 -B rank/deploy/k8s/stage-runtime.py "$HOME/.local/state/agentkiln/rank-k8s/platform-runtime" --arch arm64
python3.12 -B rank/deploy/k8s/platform-lab.py build --stage "$HOME/.local/state/agentkiln/rank-k8s/platform-runtime"
python3.12 -B rank/deploy/k8s/platform-lab.py up
node rank/deploy/k8s/verify-platform.mjs
```

`platform-lab.py up` is a maintenance upgrade: it stops gateway/Rank/control producers and drains Execution workers, then runs Execution and Rank migrations in one ordered Job before restarting applications. It causes a lab service interruption. A failed migration leaves writers stopped and requires explicit operator recovery; it does not automatically restart old binaries against an upgraded schema.

Use `--arch x64` when staging for an amd64 cluster. The deployment root declares runtime peers explicitly. Staging materializes workspace links, installs exact Linux optional-native versions with archive integrity checks, and rejects known credential filenames. `pnpm deploy` changes the workspace dependency-selection marker; staging restores the development selection afterward. Native CLI startup is verified in the Linux image. Kubernetes service-link environment injection is disabled because its `RANK_CONTROL_PORT=tcp://…` value collides with the application's numeric port setting.

The Linux image includes the pinned Landlock native helper and Bubblewrap. DSH retains its `read-only` shell policy and probes available confinement providers; it refuses execution when none works. Container creation and model-only replies do not prove that tools can run. Verify both an allowed command and a rejected write on the target kernel and container security profile.

The acceptance script opens temporary loopback forwards, configures a local model connection and system bindings, creates an Agent version, publishes a fixture endpoint and creates its API key. It calls the existing daily-news client with a deterministic source snapshot, streams two chat turns through the OpenAI SDK, inspects structured Execution input, reads an S3-backed stdout artifact, and replays terminal SSE from a cursor. It checks direct Agent trials and PostgreSQL history filters, runs the control host and replaces Rank/control Pods before retrieving prior results and continuing the conversation. It then scales workers to zero, accepts background work, replaces Rank, and scales workers up, checking that the same request completes at attempt one and old artifacts remain unchanged. Reports remain under the private lab state directory; the script never prints credentials. These checks exercise real services and DSH processes, with deterministic model responses rather than real model quality.

To inspect the interface, forward `service/rank-gateway` port `8080` to a local port. The username is `rank`; the password stays in the private `platform-lab.json` file. Control sessions use a PVC, webhook secrets use a separate PVC, and Rank business records and login sessions use its PostgreSQL schema. `rank-api` uses HTTP-only mode, `RollingUpdate` with zero unavailable replicas and one surge, 5s minimum readiness and preStop, 30s HTTP drain and 45s Pod grace. `rank-dispatcher` and control each retain one owner with `Recreate`; workers remain independently scalable. Schema-compatible API replacements use `kubectl rollout restart deployment/rank-api`; the full maintenance command still stops writers for migrations. The [rolling example](../../examples/api-rolling/README.md) verifies live streams, shared login and durable completion. Do not run the earlier fixture scaling script against the platform image: it assumes short fixture deadlines.

## Remote Sandbox experiment

After building the platform image, build the protected runtime and OpenSandbox service, then start the dedicated Docker lab. This experiment runs beside minikube and does not change its workloads. Python 3.12, Go 1.26.0, Node 24 and a local Unix Docker socket are required. The trusted lifecycle service receives that socket; Harness containers receive neither it nor a host volume.

```sh
python3.12 -B rank/deploy/k8s/sandbox-lab.py build-runtime
python3.12 -B rank/deploy/k8s/sandbox-lab.py build-server
python3.12 -B rank/deploy/k8s/sandbox-lab.py up
python3.12 -B rank/deploy/k8s/sandbox-lab.py verify
python3.12 -B rank/deploy/k8s/verify-sandbox-lifetime.py
python3.12 -B rank/deploy/k8s/verify-sandbox-storage.py
```

`build-server --pypi-index https://pypi.tuna.tsinghua.edu.cn/simple` selects an alternate package mirror if the default is unreachable. The service and execd versions are pinned; server transitive Python dependencies are resolved during image build, so retain the resulting image digest for reproducibility. The service listens only on host loopback port 18796. Its configuration, key and SQLite lifecycle store remain in the private lab state directory. `up` preserves the selected Docker network, initially `bridge`, and starts existing owned containers; it does not replace an already-created container with a new image. An explicit network change restarts the lifecycle service. For an image upgrade, stop and remove only `rank-opensandbox-lab` and `rank-opensandbox-model` on an idle lab, then run `up`; their external lifecycle state is preserved.

The server image applies a narrow, version-checked [build patch](opensandbox-proxy-only.py) to OpenSandbox 0.2.3 and sets `docker.publish_ports=false`. Sandboxes expose private network endpoints through the authenticated server proxy; they publish no host ports. Network-policy sidecars require the upstream publication mode and are rejected in this configuration. This avoids the upstream allocator probing the server container namespace while Docker binds ports in its host namespace. The installer caches the unmodified upstream image separately and records both image identities and the patch digest. If this Docker Desktop installation stalls while BuildKit resolves an imported base, `DOCKER_BUILDKIT=0 python3.12 -B rank/deploy/k8s/sandbox-lab.py build-server` uses the same Dockerfiles with the legacy builder.

The service runs inside Linux because its proxy resolves Docker bridge addresses that macOS cannot reach directly. The model container supplies deterministic responses without external model calls. `verify` runs real DSH/structured-history integration and the [packaged remote snapshot](../../examples/execution-lifecycle/README.md); a privately supplied `EXECUTION_TEST_DATABASE_URL` selects separate API/two-worker River verification. `verify-sandbox-lifetime.py` force-stops only the dedicated lifecycle service while a detached task runs, checks the protected deadline and restarts the service. Run it without concurrent sandbox work. Its report remains in the private state directory.

These containers verify lifecycle, execution integration and the writable-storage/egress profiles below, not general hostile-code isolation or a complete production tenant policy. The [report](../reports/2026-09-15-opensandbox.md) states the measured scope. Local image IDs work on this Docker daemon; another host needs an accessible registry image pinned by manifest digest. The optional minikube integration below exercises shared S3 storage on this path; physical-host scaling remains separate acceptance work.

### Bounded writable storage

The dedicated lifecycle server sets `docker.rank_readonly_runtime=true`. Its version-checked patch uses Docker read-only rootfs, requires a memory limit, disables additional swap and rejects request mounts/extensions, configured binds and image-declared volumes. The fixed Rank image preinstalls execd and protected PID 1; it needs no runtime rootfs injection. This profile accepts the packaged Rank runtime, not arbitrary upstream sandbox images.

The lab assigns a shared 128 MiB tmpfs to input, workspace and artifacts, 32 MiB to `/tmp`, and 16 MiB to `/dev/shm`. Each of the first two mounts allows 8,192 inodes. The `rank_workspace_mib`, `rank_tmp_mib`, `rank_shm_mib`, `rank_tmpfs_inodes` and `rank_log_mib` deployment fields control these limits; Docker local logs rotate two files at a 2 MiB threshold. tmpfs consumes the container memory budget. These are bounded ephemeral writable areas, not persistent block-volume quotas or an exact host-disk budget including metadata and logs.

`verify-sandbox-storage.py` runs a native UID 1000 process through the authenticated command API. It fills the shared workspace across three directories, `/tmp`, `/dev/shm` and the inode allowance, requires `ENOSPC`, then frees space and proves subsequent writes succeed. It also verifies rootfs write rejection, rejected mount/overlay/image-volume requests, configured log rotation and runtime deletion against the checked-in snapshot. The [storage report](../reports/2026-09-15-sandbox-storage.md) records the measured image identities. The [egress profile](README.md#runtime-egress) separately constrains outbound connections; independent-host capacity remains separate work.

### Runtime egress

The lifecycle profile starts the image's protected `network-init` entry point in its own network namespace. It temporarily grants NET_ADMIN to install default-deny IPv4/IPv6 filters, then replaces PID 1 with that capability and NET_RAW removed from the bounding set. Missing policy or filter setup failure prevents daemon startup. It retains [bounded storage](README.md#bounded-writable-storage) and the original absolute deadline.

`sandbox-lab.py up` creates `sandbox-egress-policy.json` in private lab state if absent. Its initial policy permits TCP 8800 in the selected Docker subnet for deterministic model/tool fixtures; all other new external connections are denied. The installer preserves an existing policy file. Review it when changing networks, and use an idle `platform-lab.py up --sandbox` upgrade after edits. API and Workers receive the same policy, frozen per accepted execution. A policy change does not alter queued work; restore compatible Workers or cancel that work explicitly.

Production policy should identify assigned service CIDRs and ports. DNS and changing provider addresses require explicit resolver/address rules or a managed gateway; these filters do not authorize domain names or inspect TLS content. They permit replies to existing connections and loopback. Incoming access control is separate. See [the decision](../../../.agents/notes/implemented/architecture/2026-09-15-sandbox-egress.md).

On an idle remote lab, run `python3.12 -B rank/deploy/k8s/verify-sandbox-network.py`. It creates local TCP/UDP targets, proves they are reachable outside the runtime, and measures allowed and denied requests through the authenticated command API. It checks missing-policy/capability startup failure, process capability bounds and target-side arrivals. It force-stops and restores only the dedicated lifecycle server, then removes its targets and runtime. IPv6 default-deny rules are inspected; reachable external IPv6 traffic is not simulated. The [network report](../reports/2026-09-15-sandbox-network.md) records exact evidence. Run platform, storage and independent-lifetime regressions after image upgrades.

### Use the remote backend in minikube

On an idle lab with no queued requests, connect the sandbox service, model fixture and allocated runtimes to the dedicated `rank-dev` Docker network, then perform the platform maintenance upgrade:

```sh
python3.12 -B rank/deploy/k8s/sandbox-lab.py up --network rank-dev
python3.12 -B rank/deploy/k8s/platform-lab.py up --sandbox
node rank/deploy/k8s/verify-platform.mjs
```

Build the current platform image before this upgrade. The installer publishes a Kubernetes Service/EndpointSlice for the external Docker lifecycle service and supplies its key only to Execution. Worker Pods run the trusted scheduler; complete Harness containers run outside those Pods and receive neither the worker filesystem nor its Kubernetes identity. Both sides can reach the deterministic model on the selected Docker network. Accepted executions retain their backend choice: switching does not migrate queued work. To return to the local backend, run `platform-lab.py up` on an idle lab.

The verifier reads the installed mode from private lab state and requires a persisted remote handle when OpenSandbox is selected. It exercises the same endpoint, chat, trial, control, S3 and restart-recovery scenarios. Kubernetes Pod readiness can precede gateway endpoint propagation; after replacing Rank, the verifier waits for health through the gateway with a two-second per-request timeout before reading durable results. Short readiness requests permit retries while service routing converges; business requests retain their normal timeout. The external Docker service remains a laptop experiment, not a Kubernetes sandbox controller or a separate physical host.

## Worker owner-loss experiment

With the complete platform installed in remote mode, no active or queued work, and autoscaling disabled, run:

```sh
python3.12 -B rank/deploy/k8s/verify-owner-loss.py
```

The verifier uses a temporary local model fixture to exercise the real DSH Bash tool. It requires a rejected write and an observed HTTP effect before killing the owning Worker process, and checks exit code 137. It also checks cancellation after owner loss and a crash after result commit against the checked-in outcome snapshot. Runtime deletion, one attempt and non-repeated effects are required. Reports and submitted records remain in private lab state; the temporary model container is removed. Do not overlap this experiment with upgrades or other verifiers. The [report](../reports/2026-09-15-owner-loss.md) records the tested kernel and security-profile limitations.

## Bounded autoscaling experiment

With the current platform image installed in remote mode, install KEDA 2.20.2 in the dedicated cluster and run the verifier on an idle lab:

```sh
python3.12 -B rank/deploy/k8s/autoscaling-lab.py install
python3.12 -B rank/deploy/k8s/verify-autoscaling.py
```

Installation pulls official images pinned by manifest digest through local Docker and imports them into minikube before Helm deployment. If Docker registry downloads stall, `install --registry-mirror ghcr.nju.edu.cn` uses the maintained Crane CLI to fetch the same fixed digests through that public mirror; Go 1.26.0 is required, and image identities are recorded in private lab state. The installer assigns owned local image tags and uses `imagePullPolicy: Never`, so the cluster consumes the verified imports. KEDA owns cluster-level CRDs and the external metrics API; its operator watches only `rank-dev`. Do not install this alongside another external metrics API provider without resolving ownership. The separate capacity token authorizes only aggregate reads. It remains in a Kubernetes Secret and is not supplied to Harness containers.

The verifier enables the [one-to-three worker policy](base/autoscaling.yaml), runs sixteen remote DSH requests against a ten-second deterministic model, observes growth and drain, then temporarily lowers the shared model concurrency to one. Blocked requests must not increase desired replicas. It cancels remaining work, confirms cleanup and restores original limits and two manually managed workers. Reports include image identities, replica/demand samples, terminal outcomes and queue latency. The [recorded experiment](../reports/2026-09-15-autoscaling.md) includes both a passing run and the preceding sandbox port-allocation failure. This is a model-waiting laptop workload, not production throughput evidence. Run it without simultaneous platform upgrades or other verification scripts.

To enable the policy for an independent experiment, run `autoscaling-lab.py enable`; `disable` removes its ScaledObject/HPA and restores two workers. API and workers must have matching backend/limits and one slot per queue. Disable autoscaling before maintenance or manual scaling; otherwise the HPA can undo a drain. The configured oldest-ready age supports diagnosis; the scaler uses quota-filtered slot demand, not raw queue count or historical p95. See [the capacity decision](../../../.agents/notes/implemented/architecture/2026-09-15-execution-capacity.md) for unsupported signals.

## Sandbox resource budget experiment

On an idle remote platform with two Worker replicas and autoscaling disabled, run:

```sh
python3.12 -B rank/deploy/k8s/verify-runtime-pool.py
```

The verifier lowers the durable `rank-dev` pool budget independently to 500 millicores and 512 MiB. Each phase submits three requests with different tenants/model connections through real DSH, checks at most one live container, inspects Docker CPU/memory limits, and requires waiting tasks to retain attempt zero. Resource-blocked work must not increase runnable demand. It compares outcomes with a checked-in snapshot, cancels remaining owned work on exit, restores the original budget and removes its model fixture. Run no upgrades, restarts or other verifiers while the temporary budget differs from startup flags. The default lab reserves 2 CPU and 2 GiB for managed Sandbox containers; this is an assigned budget on the shared laptop, not automatic host sizing.

## Queue deadline experiment

On an idle complete-platform lab with autoscaling disabled, run:

```sh
python3.12 -B rank/deploy/k8s/verify-queue-deadline.py
```

The verifier sets workers to zero, submits a one-second queue deadline and replaces the Execution API. It checks the frozen failure and SSE replay, restores two workers and completes a fresh real DSH invocation. Reports remain in private lab state; accepted records remain available for inspection. It restores workers on exit. This tests waiting-time enforcement, not a total request-latency target.

## Database permission experiment

With the complete platform running, verify restricted logins and then run the public-client regression:

```sh
python3.12 -B rank/deploy/k8s/verify-database-roles.py
node rank/deploy/k8s/verify-platform.mjs
```

The permission verifier uses real TCP authentication, creates and removes uniquely named probe tables, and checks both permitted DML and denied DDL/cross-service access. Reports remain in the private state directory. Do not run it concurrently with maintenance. The complete platform verifier also replaces Pods and changes Worker replicas; use an idle lab with autoscaling disabled.

Both installers provision separate schema owners and runtime roles while writers are stopped, run migrations with database-only Secrets, and restrict migration metadata before starting applications. Stable credentials live in the owner-only `database-roles.json` outside Git. Provisioning removes PUBLIC privileges only in this dedicated lab database. `lab.py up` refuses to replace an installed complete platform; use `platform-lab.py up` with its installed backend option. Migrations need database CREATE; runtime accounts do not. Schema ownership and grants are not a substitute for production TLS, identity, credential rotation or tenant authorization.

## Dependency interruption experiment

Run against an idle complete remote platform with autoscaling disabled:

```sh
python3.12 -B rank/deploy/k8s/verify-dependencies.py
```

The verifier first records a successful baseline artifact. After observing a real DSH tool effect, it scales the dedicated PostgreSQL Deployment to zero, waits for Pod deletion, requires an actual HTTP server error within eight seconds (three-second database budgets; a client timeout fails verification), and restores the database after the persistence window. Recovery must preserve the accepted identity, report an unknown outcome and avoid repeating the effect.

The upload scenario routes the lab object Service through a temporary unprivileged streaming proxy. Workers restart while idle to discard old S3 connections. The proxy permits the first selected artifact and interrupts subsequent request bodies after forwarding only a prefix. The failed execution must retain known output/usage and only the confirmed artifact; its bytes are verified after decoding the API response. A fresh invocation and baseline artifact read must succeed after recovery. The assembled observations are compared with [the dependency snapshot](fixtures/dependencies.snapshot.json).

The script restores PostgreSQL and the object Service selector, waits for owned execution cleanup, removes its model/proxy fixtures and records a private report. A forcibly terminated verifier cannot run its cleanup: inspect the dedicated namespace, restore `deployment/postgres` to one replica and `service/objects` selector to `app: objects`, then remove only fixtures named in the interrupted run. Neither a brief unavailable read nor a restarted Rank process is a high-availability claim. This experiment uses deterministic local models and does not test production storage failover.

## Active Worker drain experiment

On an idle complete remote platform with autoscaling disabled, run:

```sh
python3.12 -B rank/deploy/k8s/verify-active-drain.py
```

The verifier requires two Workers with one online slot each, a 15-second shutdown grace and a 25-second Pod grace. It holds real DSH Bash calls on both owners, scales from two Workers to one, and checks both completion within grace and interruption at its end. A following online request must remain queued until the departing owner exits, then run once on the surviving Worker. A rolling-update phase holds both original owners, releases each when deletion begins and requires the following request to use a new Worker.

A Kubernetes watch retains each exact Pod UID's terminal container exit status before object deletion. Pod absence alone is insufficient. The [snapshot](fixtures/active-drain.snapshot.json) requires exit code zero, one execution attempt, one tool effect per held request, one terminal lifecycle event and confirmed Sandbox deletion. The interrupted request fails with an unknown outcome; it is not reported as successful merely because the Worker exits cleanly. Completed outputs remain readable from S3 with matching digests.

The script cancels and settles only its requests, restores two Workers and the previous rollout annotations, closes its watch/port-forward and removes its model fixture. Cleanup failures make the run fail and remain in its private report. The [measured run](../reports/2026-09-15-active-drain.md) records the actual grace timings. This exercises manual scale-down and Kubernetes rolling replacement with live tasks; automatic KEDA policy selection, node loss and physical-host capacity are separate tests. Production Pod grace must leave room for draining, bounded runtime cleanup, persistence and shutdown overhead.

The complete-platform verifier also requests a one-call DSH budget against a deterministic model that keeps returning HTTP 429. It compares the failed Execution, retained limit, idempotent replay and provider-side request count with [the budget snapshot](fixtures/model-budget.snapshot.json). Run it after upgrading both Execution roles and the Sandbox image; the local model fixture must use the same platform build. [Budget configuration](../../../execution/backend/README.md#model-request-budget) defines supported adapters and the separate RPM/TPM limitations.

The platform verifier also runs [the access-control scenario](../../examples/access-control/README.md), checks the authenticated publication actor, adds and removes a temporary operator in the projected file, and verifies scoped control-service access through private service addresses. It waits up to three minutes for each Kubernetes Secret projection and restores the original allowlist. This proves shared-workspace operator access and public tenant/endpoint restrictions; it does not add team ownership to Agent or Dataset definitions.

## Shared quotas and remaining production checks

The platform configures a separate invocation-signing key outside Git. Remote Sandboxes reach only the permit path through the lab gateway's port 30091; their egress plan admits that exact node and port. This plain HTTP route is private lab plumbing. The [production reference](production.md) defines TLS, identity, storage, migration, retention and capacity requirements.

Run these checks on an idle remote platform, one at a time:

```sh
node rank/deploy/k8s/verify-model-quota.mjs --keda
python3.12 -B rank/deploy/k8s/verify-recovery-windows.py
python3.12 -B rank/deploy/k8s/verify-fresh-database.py
python3.12 -B rank/deploy/k8s/verify-database-roles.py
python3.12 -B rank/deploy/k8s/verify-active-drain.py --keda
```

Quota verification uses two connections and excludes seven quota-blocked jobs from KEDA demand. Recovery verification kills a real Worker during database loss and after S3 commit but before reference publication; it preserves known reply/usage, refuses replay and identifies the unreferenced object without exposing it through the artifact API. Fresh-database verification creates and removes an isolated database and runs packaged migrations twice. Automatic-drain verification uses a zero-second stabilization overlay and Pod deletion cost to force selection of an active owner; the normal lab scaler retains its 45-second stabilization. Both grace completion and grace expiry check task outcome, exit status and Sandbox cleanup. The scripts restore two Workers and remove their temporary scalers; they do not delete accepted execution history.

Real DeepSeek acceptance is explicitly opt-in: `node rank/deploy/k8s/verify-model-quota.mjs --deepseek` requires an owner-only `deepseek-acceptance.json` in the private lab state directory with `apiKey`, `baseURL: "https://api.deepseek.com"`, `maxRequests: 10` and `maxOutputTokens: 512`. A temporary fixed-upstream proxy records debits in `deepseek-proxy/deepseek-ledger.json` before dispatch and retains the total across restarts. Never reset this ledger to retry acceptance. Connection verification counts against the same total. The actual news source and two-turn SSE chat use the public SDK through Rank and remote DSH. The proxy is removed afterwards, so its temporary model connection is not a reusable production connection. Credentials stay outside Git and reports. The [local quota example](../../examples/model-quota/README.md) supplies the operator configuration reference.

The gateway serves the login shell publicly; Rank and control-host authenticate business requests. Open the forwarded URL, enter account `rank` and the generated `GATEWAY_PASSWORD` from the owner-only `platform-lab.json`, then sign out through Settings. Keep the same frontend origin for API and control routes. A Rank restart requires a fresh browser login; see [management access](../../backend/README.md#management-access).

With the gateway forwarded to port 18780, run `node rank/deploy/k8s/verify-browser-login.mjs`. It reads the existing owner-only lab credential, verifies cookie access, control-host forwarding, origin rejection and logout, and makes no model calls. `RANK_LOGIN_TEST_URL` overrides the forwarded URL. It creates one named acceptance conversation and leaves it for inspection.
