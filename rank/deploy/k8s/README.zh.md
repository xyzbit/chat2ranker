# 本地 Execution Kubernetes 实验

[English](README.md) | 中文

本教程的第一部分部署可信 Execution 部分：PostgreSQL、SeaweedFS S3 服务、一个 Execution API 和两个 Worker Pod。镜像运行[确定性 CLI 案例](../../examples/execution-lifecycle/README.md)，不包含 DSH、Rank、Web 网关或生产沙箱，用于在平台集成之前验证进程分布与证据保留。

要端到端体验产品，先按下文启动基础服务，再运行[完整平台实验](README.md#complete-platform-experiment)中的热点和聊天客户端；验证运行时隔离与 Worker 容量时，再接入[远程 Sandbox](README.md#remote-sandbox-experiment)和[有界自动扩缩容](README.md#bounded-autoscaling-experiment)。已经安装完整平台时，用 `platform-lab.py up` 升级，并保留已选的 `--sandbox`；不要用 `lab.py up` 降级覆盖。

## 准备与构建

使用 Docker、Go 1.26.0、Python 3.12 或更新版本、kubectl 和 minikube。启动 Docker，并为拟定的 4 CPU、4 GiB 集群保留足够内存。在仓库根目录运行：

```sh
mkdir -p "$HOME/.local/state/agentkiln/rank-k8s"
KUBECONFIG="$HOME/.local/state/agentkiln/rank-k8s/config" minikube start -p rank-dev --driver=docker --cpus=4 --memory=4096
python3.12 rank/deploy/k8s/lab.py build
```

构建器按宿主架构交叉编译原生 Linux 二进制，下载 SeaweedFS v4.47 并核对发行校验值，然后构建按内容标记的 fixture（测试前置数据）镜像。Node 基础镜像和 PostgreSQL 依赖按摘要锁定。PostgreSQL 在导入 containerd 前会获得专用实验标签，因为只有摘要的 Docker 归档不会保留可运行的仓库标签。应用 Pod 使用导入的镜像，配置 `imagePullPolicy: Never`。在架构不同的构建宿主上，传入 `--arch arm64` 或 `--arch amd64`。构建产物与镜像标识保存在 Git 之外的私有实验状态目录中。

## 部署与检查

```sh
python3.12 rank/deploy/k8s/lab.py up
python3.12 rank/deploy/k8s/lab.py status
kubectl --kubeconfig="$HOME/.local/state/agentkiln/rank-k8s/config" --context=rank-dev -n rank-dev port-forward --address=127.0.0.1 service/execution-api 18790:8790
```

安装器等待节点就绪后，再从本地 Docker daemon 导入镜像。如果网络插件无法下载镜像，先解决集群就绪问题；延长应用滚动更新超时不能修复这个问题。安装器只操作专用的 `rank-dev` context 和 namespace。生成的数据库、S3 与加密凭据保存在权限为 0600 的本地文件中；安装器导入镜像、启动持久依赖、显式执行迁移 Job，再启动 API 和 Worker。等待命令有时间上限，依赖或迁移失败会停止安装，需要检查相应资源后再重试。失败的迁移 Job 需要运维者显式处理；再次应用同一个不可变 Job 不会重置失败状态。

转发后的 Execution API 需要私有 `execution-lab.json` 状态文件中生成的服务令牌。通过本地环境或直接读取该文件的客户端使用，不要将其粘贴到对话或命令参数中。队列限制、超时、资源请求与 Worker 槽位配置位于[清单](base/execution.yaml)。Rank 和 Execution 使用独立的受限日常账号，拥有者凭据只提供给迁移 Job，详见[数据库权限决策](../../../.agents/notes/implemented/architecture/2026-09-15-rank-database-permissions.md)。对象服务就绪检查只确认监听端口，Execution 会在启动时独立核对存储桶。

两个 Worker Pod 不共享工作目录或产物暂存目录，数据库与对象 PVC 在替换 Worker 后继续保留。单节点实验不能证明物理主机吞吐或存储高可用。完整运行时隔离与物理主机扩容仍是[生产设计](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md)中的独立验收要求。

## 运行验收实验

部署后运行以下命令。它使用临时回环端口转发，直接从实验状态文件读取私有服务令牌。

```sh
python3.12 -B rank/deploy/k8s/verify.py
```

实验只将专用实验的 Worker 调整为零、一个和两个副本，替换 API、Worker 与持久依赖 Pod，并在可信 fixture 调用中强制移除一个 Worker。请在空闲实验环境运行。请求、事件和对象保留供检查，退出时恢复两个 Worker 副本。仓库外实验状态目录中的 JSON 报告记录镜像标识、请求 ID、重放和持久化检查、实际执行拥有者、批次耗时与排队时延。一个与两个 Worker 的比较只测量当前笔记本上的 fixture，不是生产容量目标。

仅停止并启动 `rank-dev` profile 后，可以不提交新任务，直接核对保留的证据。将报告路径替换为上方完整验收成功时打印的报告路径：

```sh
python3.12 -B rank/deploy/k8s/verify.py --retained-report /path/to/passed-report.json
```

依赖启动期间 Execution 容器可能重启，随后才就绪。这个只读检查等待 API 滚动更新完成，核对同一个已完成调用、SSE 历史、产物摘要及未重放的未知结果，不测量重启期间的可用性。

## 完整平台实验

完整实验增加一个 Rank API、一个持久化中控服务、静态 Nginx 网关和确定性的模型协议服务。它用真实 DSH 运行时替换 Execution 测试镜像，保留 PostgreSQL、River 和 S3。默认后端面向可信共享工作区；下文的可选远程后端增加完整运行时限制。两者均不代表生产团队隔离。浏览器使用账号密码登录页与 HttpOnly 会话 Cookie；API 客户端可通过 Basic 或 Bearer 使用带主体的管理员令牌；对外 `/v1` 调用使用端点 API Key。Rank 使用投影的 `rank-management` 摘要文件验证管理员身份，control-host 在接受公开对话请求前检查 Rank 授权。网关不转发内部中控命令。端口转发只绑定本机回环地址。

构建宿主运行时与 Web 应用，物化部署依赖，然后构建并安装平台镜像。每份运行时快照使用新的暂存目录。安装器依赖前面 `lab.py up` 启动的 Execution 基础服务。

```sh
pnpm run build:lib:host
pnpm --filter @xyzbit/agentkiln-web run build
python3.12 -B rank/deploy/k8s/stage-runtime.py "$HOME/.local/state/agentkiln/rank-k8s/platform-runtime" --arch arm64
python3.12 -B rank/deploy/k8s/platform-lab.py build --stage "$HOME/.local/state/agentkiln/rank-k8s/platform-runtime"
python3.12 -B rank/deploy/k8s/platform-lab.py up
node rank/deploy/k8s/verify-platform.mjs
```

`platform-lab.py up` 是维护升级：先停止网关、Rank、中控等任务来源并排空 Execution Worker，再由一个顺序执行的 Job 完成 Execution 与 Rank 迁移，随后启动应用。实验服务会短暂中断。迁移失败时保持写入者停止，需要运维显式恢复，不自动让旧二进制连接升级后的 schema。

为 amd64 集群暂存时使用 `--arch x64`。部署根显式声明运行时对等依赖。暂存过程物化工作区链接，按精确版本安装 Linux 可选原生依赖并验证归档完整性，同时拒绝已知凭证文件名。`pnpm deploy` 会改变工作区依赖选择标记；暂存后恢复开发依赖选择。原生 CLI 在 Linux 镜像中验证启动。禁用 Kubernetes 服务链接环境注入，因为它注入的 `RANK_CONTROL_PORT=tcp://…` 与应用的数字端口配置冲突。

Linux 镜像包含固定版本的 Landlock 原生程序和 Bubblewrap。DSH 保留 `read-only` Shell 策略并探测可用的隔离实现；没有可用实现时拒绝执行。容器创建成功或仅模型回复成功都不代表工具可用。需要在目标内核及容器安全配置下，同时验证允许的命令与被拒绝的写入。

验收脚本启动临时回环端口转发，配置本地模型连接与系统绑定，创建 Agent 版本，发布测试端点并创建 API Key。它用确定性来源快照调用现有每日热点客户端，通过 OpenAI SDK 流式运行两轮聊天，检查结构化 Execution 输入，读取 S3 支持的 stdout 产物，并按游标重放终态 SSE。它检查直接 Agent 试用及 PostgreSQL 历史筛选，还运行中控服务，替换 Rank/中控 Pod，再读取已有结果并继续对话。 最后将 Worker 缩到零，接受后台请求并替换 Rank，再扩容 Worker，验证同一请求以第一次尝试完成且旧产物保持一致。报告保存在私有实验状态目录；脚本不会打印凭证。这些检查使用真实服务和 DSH 进程，以及确定性模型回复，不验证真实模型质量。

检查界面时，将 `service/rank-gateway` 的 `8080` 端口转发到本机端口。用户名为 `rank`，密码保存在私有 `platform-lab.json` 文件。中控会话使用 PVC，Webhook 密钥使用独立 PVC，Rank 业务记录与登录会话使用自己的 PostgreSQL schema。`rank-api` 使用纯 HTTP 模式，采用零不可用副本、一个额外副本的 `RollingUpdate`，最短就绪和 preStop 各 5 秒、HTTP 排空 30 秒、Pod 宽限期 45 秒。`rank-dispatcher` 与中控各保留一个拥有者并采用 `Recreate`；Worker 仍可独立扩缩容。schema 兼容的 API 替换使用 `kubectl rollout restart deployment/rank-api`；完整维护命令仍停止写入者后执行迁移。[滚动更新案例](../../examples/api-rolling/README.md)验证活跃流、共享登录和持久任务完成。不要对平台镜像运行前面的测试负载扩容脚本：它依赖短时 fixture 截止时间。

## 远程 Sandbox 实验

平台镜像构建完成后，构建受保护的运行时与 OpenSandbox 服务，再启动专用 Docker 实验。此实验与 minikube 并行存在，不修改其工作负载。需要 Python 3.12、Go 1.26.0、Node 24 及本地 Unix Docker socket。可信生命周期服务获得该 socket；Harness 容器既不获得它，也不挂载主机卷。

```sh
python3.12 -B rank/deploy/k8s/sandbox-lab.py build-runtime
python3.12 -B rank/deploy/k8s/sandbox-lab.py build-server
python3.12 -B rank/deploy/k8s/sandbox-lab.py up
python3.12 -B rank/deploy/k8s/sandbox-lab.py verify
python3.12 -B rank/deploy/k8s/verify-sandbox-lifetime.py
python3.12 -B rank/deploy/k8s/verify-sandbox-storage.py
```

默认包源无法访问时，`build-server --pypi-index https://pypi.tuna.tsinghua.edu.cn/simple` 可选择镜像源。服务与 execd 版本固定；服务端 Python 间接依赖在镜像构建时解析，因此需保留最终镜像摘要以便复现。服务只监听主机回环地址的 18796 端口，配置、密钥与 SQLite 生命周期存储保存在私有实验状态目录。`up` 保留已选的 Docker 网络，首次默认为 `bridge`，并启动已有的所属容器，不会用新镜像替换已创建容器。显式切换网络会重启生命周期服务。镜像升级时，在空闲实验环境中只停止并移除 `rank-opensandbox-lab` 和 `rank-opensandbox-model`，然后重新执行 `up`；仓库外的生命周期状态会保留。

服务镜像对 OpenSandbox 0.2.3 应用范围明确、检查版本的[构建补丁](opensandbox-proxy-only.py)，并设置 `docker.publish_ports=false`。Sandbox 通过鉴权服务端代理暴露私网端点，不映射宿主机端口。网络策略 sidecar 需要上游端口映射模式，在当前配置下会被拒绝。这避免上游分配器在服务容器的网络命名空间探测端口，而 Docker 在宿主机命名空间绑定端口造成冲突。安装程序单独缓存未修改的上游镜像，记录两个镜像标识和补丁摘要。如果当前 Docker Desktop 的 BuildKit 在解析已导入基础镜像时停滞，可用 `DOCKER_BUILDKIT=0 python3.12 -B rank/deploy/k8s/sandbox-lab.py build-server` 通过旧构建器执行相同 Dockerfile。

服务在 Linux 内运行，因为其代理使用 macOS 无法直接访问的 Docker 桥接地址。模型容器提供确定性回复，不调用外部模型。`verify` 运行真实 DSH/结构化历史集成与[打包远程快照](../../examples/execution-lifecycle/README.md)；私有环境中的 `EXECUTION_TEST_DATABASE_URL` 可选择独立 API/两个 Worker 的 River 验证。`verify-sandbox-lifetime.py` 在脱离进程组的任务运行时，只强制停止专用生命周期服务，检查受保护的截止时间并重启服务。运行期间不要并发执行其他 Sandbox 任务，报告保存在私有状态目录。

这些容器验证生命周期、执行集成及下述可写存储/出口配置，不验证通用恶意代码隔离或完整生产租户策略。[报告](../reports/2026-09-15-opensandbox.md)说明实测范围。本地镜像 ID 只适用于当前 Docker daemon；另一台主机需要可访问且按 manifest 摘要固定的仓库镜像。下方可选的 minikube 集成验证该路径上的共享 S3 存储；物理主机扩容仍需独立验收。

### 有界可写存储

专用生命周期服务设置 `docker.rank_readonly_runtime=true`。检查版本的补丁使用 Docker 只读根文件系统，要求内存上限，禁止额外 swap，并拒绝请求挂载、扩展、服务配置挂载和镜像声明的卷。固定 Rank 镜像预装 execd 与受保护的 PID 1，无需在运行时注入根目录。此配置接受打包的 Rank 运行时，不接受任意上游 Sandbox 镜像。

实验给输入、工作区和产物共同分配 128 MiB tmpfs，给 `/tmp` 分配 32 MiB，给 `/dev/shm` 分配 16 MiB；前两个挂载各允许 8,192 个 inode。部署字段 `rank_workspace_mib`、`rank_tmp_mib`、`rank_shm_mib`、`rank_tmpfs_inodes` 和 `rank_log_mib` 控制这些限制；Docker local 日志按 2 MiB 阈值保留两个轮转文件。tmpfs 消耗容器内存预算。这些是有界临时可写区域，不是持久块存储配额，也不是包含元数据和日志的精确宿主机磁盘预算。

`verify-sandbox-storage.py` 通过鉴权命令 API 运行 UID 1000 的原生进程，跨三个目录写满共享工作区，并写满 `/tmp`、`/dev/shm` 和 inode 额度；要求返回 `ENOSPC`，清理后还必须能继续写入。它也按仓库快照验证根目录写入拒绝、挂载/覆盖层/镜像卷请求拒绝、日志轮转配置与运行时删除。[存储报告](../reports/2026-09-15-sandbox-storage.md)记录实测镜像身份。[出口配置](README.md#runtime-egress)单独约束外连；独立主机容量仍需单独完成。

### 运行时网络出口

生命周期配置在独立网络命名空间中启动镜像的受保护 `network-init` 入口。它临时授予 NET_ADMIN，以安装默认拒绝的 IPv4/IPv6 过滤规则，随后替换 PID 1，并从能力边界集合移除 NET_ADMIN 和 NET_RAW。缺少策略或过滤器安装失败时，不启动守护进程。[有界存储](README.md#bounded-writable-storage)与原有绝对截止时间继续生效。

`sandbox-lab.py up` 在私有实验状态目录中首次创建 `sandbox-egress-policy.json`。初始策略允许所选 Docker 子网中的 TCP 8800，用于确定性模型/工具服务；其他新外连全部拒绝。安装器保留已存在的策略文件。切换网络时检查它，修改后在空闲环境执行 `platform-lab.py up --sandbox` 升级。API 与 Worker 使用相同策略，并按已接受的 Execution 冻结。修改策略不会改变排队任务；需要恢复兼容 Worker 或显式取消这些任务。

生产策略应明确分配的服务 CIDR 与端口。DNS 和变化的供应商地址需要明确的解析器/地址规则或托管网关；这些过滤器不做域名授权或 TLS 内容检查。已有连接的回复与回环访问允许通过，入站访问控制单独处理。依据见[决策记录](../../../.agents/notes/implemented/architecture/2026-09-15-sandbox-egress.md)。

在空闲远程实验环境运行 `python3.12 -B rank/deploy/k8s/verify-sandbox-network.py`。它创建本地 TCP/UDP 目标，证明目标在运行时之外可达，再通过鉴权命令 API 测量允许与拒绝的请求。它检查缺少策略/能力时的启动失败、进程能力边界与目标端请求记录；只强制停止并恢复专用生命周期服务，最后移除目标服务和运行时。IPv6 默认拒绝规则通过检查确认，未模拟可达的外部 IPv6 流量。[网络报告](../reports/2026-09-15-sandbox-network.md)记录精确证据。镜像升级后，执行平台、存储与独立寿命回归。

### 在 minikube 中使用远程后端

在没有排队请求的空闲实验环境中，将 Sandbox 服务、模型 fixture 与分配的运行时连接到专用的 `rank-dev` Docker 网络，再执行平台维护升级：

```sh
python3.12 -B rank/deploy/k8s/sandbox-lab.py up --network rank-dev
python3.12 -B rank/deploy/k8s/platform-lab.py up --sandbox
node rank/deploy/k8s/verify-platform.mjs
```

升级前需构建当前平台镜像。安装器通过 Kubernetes Service/EndpointSlice 暴露外部 Docker 生命周期服务，只向 Execution 提供其密钥。Worker Pod 运行可信调度程序；完整 Harness 容器在这些 Pod 外运行，既不获得 Worker 文件系统，也不获得其 Kubernetes 身份。两端都能通过选定 Docker 网络访问确定性模型。已接受执行保留原来的后端选择：切换后端不会迁移排队工作。回到本地后端时，在空闲环境执行 `platform-lab.py up`。

验证器从私有实验状态读取已安装模式，选择 OpenSandbox 时要求存在持久化远程句柄。它验证相同的端点、聊天、试用、中控、S3 与重启恢复场景。Kubernetes Pod 就绪可能早于网关端点传播；替换 Rank 后，验证器先用单次两秒超时等待网关健康检查，再读取持久结果。短就绪请求允许路由收敛期间重试，业务请求保留正常超时。外部 Docker 服务仍是笔记本实验，不是 Kubernetes Sandbox 控制器，也不是独立物理主机。

## Worker 拥有者丢失实验

完整平台已安装为远程模式、没有运行中或排队任务且自动扩缩容已禁用时，运行：

```sh
python3.12 -B rank/deploy/k8s/verify-owner-loss.py
```

验证器使用临时本地模型 fixture 驱动真实 DSH Bash 工具。它要求先观察到写入拒绝与 HTTP 操作，再强制停止拥有者 Worker 进程，并检查退出码 137。它还对照仓库内的结果快照，检查拥有者丢失后的取消和结果提交后的崩溃，要求运行时删除、只有一次尝试、外部操作不重复。报告与提交记录保留在私有实验状态中，临时模型容器会被移除。不要同时执行升级或其他验证器。[报告](../reports/2026-09-15-owner-loss.md)记录已测内核与安全配置限制。

## 有界自动扩缩容实验

将当前平台镜像以远程模式安装后，在专用集群安装 KEDA 2.20.2，并在空闲实验环境运行验证：

```sh
python3.12 -B rank/deploy/k8s/autoscaling-lab.py install
python3.12 -B rank/deploy/k8s/verify-autoscaling.py
```

安装先通过本机 Docker 拉取由 manifest 摘要固定的官方镜像并导入 minikube，再部署 Helm。Docker 镜像下载停滞时，可以使用 `install --registry-mirror ghcr.nju.edu.cn`，通过维护中的 Crane CLI 从这一公共镜像站获取相同的固定摘要；此路径需要 Go 1.26.0，并将镜像标识保存在私有实验状态目录。安装程序使用专用本地镜像标签和 `imagePullPolicy: Never`，确保集群使用已核对的导入镜像。KEDA 拥有集群级 CRD 和 external metrics API；operator 只监听 `rank-dev`。与其他 external metrics API provider 共存前必须解决拥有权冲突。独立容量令牌只授权聚合读取，保存在 Kubernetes Secret 中，不传给 Harness 容器。

验证脚本启用[一到三个 Worker 的策略](base/autoscaling.yaml)，对延迟十秒的确定性模型提交十六个远程 DSH 请求，观察扩容和排空，然后临时把共享模型并发降为一。被额度阻塞的请求不得提高期望副本数。最后取消剩余工作、确认清理，恢复原有限额和手动管理的两个 Worker。报告包含镜像标识、副本/需求采样、终态和排队时延。[实测记录](../reports/2026-09-15-autoscaling.md)同时保留成功结果及此前 Sandbox 端口分配失败。这是在笔记本上等待模型回复的负载，不是生产吞吐证据。不要同时执行平台升级或其他验证脚本。

独立实验可运行 `autoscaling-lab.py enable`；`disable` 移除其 ScaledObject/HPA 并恢复两个 Worker。API 和 Worker 必须使用相同后端/限额，且每个队列各一个槽位。维护和手动扩缩容前先停用自动扩缩容，否则 HPA 可能撤销排空操作。最老可执行年龄用于诊断；扩容信号使用扣除额度阻塞后的槽位需求，不使用队列总数或历史 p95。不支持的信号见[容量决策](../../../.agents/notes/implemented/architecture/2026-09-15-execution-capacity.md)。

## Sandbox 资源预算实验

远程平台空闲、运行两个 Worker 副本且自动扩缩容已禁用时，运行：

```sh
python3.12 -B rank/deploy/k8s/verify-runtime-pool.py
```

验证器分别将持久化的 `rank-dev` 资源池限制为 500 毫核和 512 MiB。每个阶段通过真实 DSH 提交三个不同租户/模型连接的请求，检查最多一个存活容器、核对 Docker CPU/内存限制，并要求等待任务保持 attempt 为零。资源阻塞的任务不能增加可执行需求。它对照仓库内的结果快照，退出时取消剩余所属工作、恢复原预算并删除模型 fixture。临时预算与启动参数不一致期间，不要运行升级、重启或其他验证器。实验默认给受管理 Sandbox 容器分配 2 CPU 和 2 GiB；这是共享笔记本上的预算分配，不是自动主机容量测量。

## 排队截止时间实验

在空闲、已关闭自动扩缩容的完整平台实验环境中运行：

```sh
python3.12 -B rank/deploy/k8s/verify-queue-deadline.py
```

脚本将 Worker 设为零，提交排队截止时间为一秒的请求，并替换 Execution API。它验证固定失败结果与 SSE 重放，恢复两个 Worker 后完成一次新的真实 DSH 调用。报告保存在私有实验状态目录，已接受记录保留供检查；退出时恢复 Worker。本实验验证等待时限，不代表请求总时延目标。

## 数据库权限实验

完整平台运行后，验证受限账号，再运行公开客户端回归：

```sh
python3.12 -B rank/deploy/k8s/verify-database-roles.py
node rank/deploy/k8s/verify-platform.mjs
```

权限验证器使用真实 TCP 鉴权，创建并移除随机命名的探测表，同时检查允许的 DML 和被拒绝的 DDL、跨服务访问。报告保存在私有状态目录。不要与维护升级并发执行。完整平台验证器还会替换 Pod、调整 Worker 副本数，需要实验环境空闲且关闭自动扩缩容。

两种安装器在写入者停止时配置独立 schema 拥有者和日常账号，用仅含数据库凭据的 Secret 执行迁移，并在启动应用前限制迁移元数据权限。稳定凭据保存在 Git 外、仅文件拥有者可读写的 `database-roles.json` 中。权限配置只在专用实验数据库内移除 PUBLIC 授权。`lab.py up` 拒绝替换已安装的完整平台；应使用 `platform-lab.py up` 并保留已安装的后端选项。迁移需要数据库 CREATE，日常账号不需要。Schema 拥有权和授权不能替代生产 TLS、身份、凭据轮换或租户授权。

## 依赖中断实验

在完整远程平台空闲、自动扩缩容关闭时运行：

```sh
python3.12 -B rank/deploy/k8s/verify-dependencies.py
```

验证器先记录一个成功的基线产物。观察到真实 DSH 工具操作后，将专用 PostgreSQL Deployment 缩为零，等待 Pod 删除，要求八秒内收到实际 HTTP 服务端错误（三秒数据库预算；客户端超时不算通过），并在持久化窗口过去后恢复数据库。恢复必须保留已接受调用的身份、明确报告结果未知，且不能重复外部操作。

上传场景将实验对象 Service 路由到临时的非特权流式代理。Worker 在空闲时重启，清除旧 S3 连接。代理允许首个选定产物通过，对后续请求只转发前缀后中断请求体。失败执行必须保留已知输出、用量及唯一已确认产物；解码 API 响应后校验其字节。恢复后，新调用和基线产物读取必须成功。组装后的观察结果与[依赖快照](fixtures/dependencies.snapshot.json)比较。

脚本恢复 PostgreSQL 和对象 Service 选择器，等待所属执行清理，移除模型、代理 fixture 并记录私有报告。验证器被强制终止时无法执行清理：检查专用命名空间，将 `deployment/postgres` 恢复到一个副本，将 `service/objects` 选择器恢复为 `app: objects`，然后只移除该次运行命名的 fixture。短时读取不可用或 Rank 进程重启都不构成高可用声明。本实验使用本地确定性模型，不验证生产存储故障切换。

## 活跃 Worker 排空实验

完整远程平台空闲且自动扩缩容关闭时运行：

```sh
python3.12 -B rank/deploy/k8s/verify-active-drain.py
```

验证器要求两个 Worker，各有一个在线槽位，退出宽限期 15 秒，Pod 宽限期 25 秒。它在两个拥有者上保持真实 DSH Bash 调用，将 Worker 从两个缩为一个，分别检查宽限期内完成及到期中断。随后提交的在线请求必须等离开的拥有者退出后，由幸存 Worker 执行一次。滚动更新场景保持两个原拥有者的任务，在各自开始删除时释放对应调用，并要求后续请求由新 Worker 接手。

Kubernetes watch 在 Pod 对象删除前，保留精确 Pod UID 对应的容器终态；只有 Pod 消失不算通过。[快照](fixtures/active-drain.snapshot.json)要求退出码为零、一次执行尝试、每个保持中的请求只产生一次工具操作、唯一生命周期终态及确认 Sandbox 删除。被中断请求以结果未知失败，不能因为 Worker 干净退出就算业务成功。已完成输出仍可从 S3 读取且摘要一致。

脚本只取消并等待自己的请求，恢复两个 Worker 及原滚动更新注解，关闭 watch 和端口转发，移除模型 fixture。清理失败会让实验失败，并保留在私有报告中。[实测记录](../reports/2026-09-15-active-drain.md)提供实际退出耗时。本实验覆盖活跃任务下的手动缩容及 Kubernetes 滚动替换；KEDA 自动策略选择、节点丢失和物理主机容量另行验证。生产 Pod 宽限期必须为排空、有界运行时清理、持久化及退出开销留足时间。

完整平台验证器还向一个持续返回 HTTP 429 的确定性模型请求仅允许一次调用的 DSH 预算。它将失败的 Execution、保留的上限、幂等重提及供应商侧请求数与[预算快照](fixtures/model-budget.snapshot.json)比较。升级两种 Execution 角色和 Sandbox 镜像后运行；本地模型服务必须使用同一平台构建。[预算配置](../../../execution/backend/README.md#model-request-budget)定义支持的适配器及独立的 RPM/TPM 限制。

平台验证器还运行[访问控制场景](../../examples/access-control/README.md)，检查已认证发布身份，在投影文件中添加并移除临时管理员，并通过私网服务地址验证中控权限。每次 Kubernetes Secret 投影最多等待三分钟，结束后还原原白名单。这证明共享工作区管理员访问和对外租户/端点限制，不会为 Agent 或 Dataset 定义增加团队归属。

## 共享额度与剩余生产检查

平台在 Git 之外保存独立的调用签名密钥。远程 Sandbox 通过实验网关端口 30091 访问额度许可路径；出口计划仅放行对应节点及端口。此明文 HTTP 路由只用于私有实验。[生产参考](production.md)定义 TLS、身份、存储、迁移、保留及容量要求。

在空闲的远程平台上依次运行，不并发注入故障：

```sh
node rank/deploy/k8s/verify-model-quota.mjs --keda
python3.12 -B rank/deploy/k8s/verify-recovery-windows.py
python3.12 -B rank/deploy/k8s/verify-fresh-database.py
python3.12 -B rank/deploy/k8s/verify-database-roles.py
python3.12 -B rank/deploy/k8s/verify-active-drain.py --keda
```

额度验证使用两个连接，并将七个额度阻塞任务排除在 KEDA 需求外。恢复验证在数据库失联时、S3 已提交而引用尚未发布时杀死真实 Worker；保留已知回答与用量，拒绝重放，并识别未引用对象而不通过产物 API 暴露它。空数据库验证创建并删除独立数据库，将打包迁移运行两次。自动排空验证使用零秒稳定窗口覆盖及 Pod 删除优先级，强制选择活跃拥有者；常规实验仍保留 45 秒稳定窗口。宽限期内完成与宽限期到期两种情况均核对任务结果、退出状态和 Sandbox 清理。脚本恢复两个 Worker 并移除临时扩缩容配置，不删除已接受的执行历史。

真实 DeepSeek 验证需要显式执行 `node rank/deploy/k8s/verify-model-quota.mjs --deepseek`。私有实验状态目录需存在仅拥有者可读的 `deepseek-acceptance.json`，包含 `apiKey`、`baseURL: "https://api.deepseek.com"`、`maxRequests: 10` 和 `maxOutputTokens: 512`。临时固定上游转发器在派发前将计数保存至 `deepseek-proxy/deepseek-ledger.json`，重启保留总数。不要通过重置账本重试验收；连接验证也计入同一上限。真实新闻来源及两轮 SSE 聊天通过公共 SDK、Rank 与远程 DSH 执行。结束后移除转发器，因此其临时模型连接不能作为持续可用的生产连接。凭据不进入 Git 或报告。[本地限流案例](../../examples/model-quota/README.md)提供运维配置参考。

网关公开提供登录页面；业务请求由 Rank 与 control-host 认证。打开端口转发地址，输入账号 `rank` 及仅所有者可读的 `platform-lab.json` 中生成的 `GATEWAY_PASSWORD`，在设置中退出。API 与中控路由保持和前端同源。Rank 重启后浏览器需要重新登录；详见[管理访问](../../backend/README.md#management-access)。

将网关转发到端口 18780 后，运行 `node rank/deploy/k8s/verify-browser-login.mjs`。它读取现有仅所有者可读的实验凭据，验证 Cookie 访问、中控转发、来源拒绝与退出，不调用模型。`RANK_LOGIN_TEST_URL` 可覆盖转发地址。它创建一个具名验收会话并保留供检查。
