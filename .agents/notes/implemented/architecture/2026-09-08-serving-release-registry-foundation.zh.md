# Agent Note: 对外服务发布注册表的标识符与别名历史

Status: implemented

[English](2026-09-08-serving-release-registry-foundation.md) | 中文

## Problem

Agent 对外服务既需要稳定的公开引用和高效的关系存储，也必须能审计某个别名究竟选择了哪个不可变 Agent 版本。全 UUID 物理模型会放大高频索引，随机 UUIDv4 写入也不利于 B-Tree 局部性；公开自增标识符则会暴露存储顺序，并让客户端耦合到单个数据库。只保存一个可变的 `production_version_id` 无法证明历史路由决定，也不能安全拒绝并发晋级。

## Decision

Rank SQLite Repository 的 schema v9 增加 `deployments`、`deployment_aliases` 和 `release_revisions`。这些对外服务表使用自动分配的 `INTEGER PRIMARY KEY` Row ID 建立物理关系，并另外使用带前缀的 UUIDv7 `public_id` 作为应用、API、事件和未来跨服务引用。物理主键不会跨越 Repository，也不需要永久禁止复用，因此 SQLite 不使用约束更强的 `AUTOINCREMENT` 关键字。领域层把 Deployment、DeploymentAlias 和 ReleaseRevision 标识符定义为不同的 Go 字符串类型。生产 PostgreSQL Adapter 使用同样的逻辑拆分，物理主键改为生成的 `BIGINT` Identity。

Deployment 是租户范围内属于一个 AgentFamily 和一份策略摘要的稳定服务目标。它拥有 `latest`、`staging` 和 `production` DeploymentAlias。每个 Alias 保存一个可空的内部当前 ReleaseRevision 指针；ReleaseRevision 不可修改，记录 Alias 范围内的修订序号、精确 AgentVersion、前序修订、可选评测 Run、策略结果、操作者、原因和时间。

Alias 推进是一次 Compare-and-Swap。调用方提交预期的公开 ReleaseRevision 标识符；Repository 校验当前指针，插入下一条修订，并且只在内部指针仍然匹配时更新 Alias。调用方没有开启事务时，`AppendReleaseRevision` 自行开启事务，因此当前指针更新失败时不会提交孤立修订。回滚通过追加一条重新指向旧 AgentVersion 的修订完成，不重写历史。

创建 Deployment 时，`latest` Alias 初始化为 AgentFamily 当前版本。创建后续 AgentVersion 时，同一个 Rank 事务会推进所有对应 Deployment 的系统维护 `latest` Alias。`staging` 与 `production` 只能由可信 Release Controller 的决定推进。策略通过必须引用已完成、评测完整且属于精确 AgentVersion 的 Run；人工覆盖必须记录操作者和原因。`customer-support@v17` 等精确引用直接解析，因此不会虚构 ReleaseRevision；别名引用同时保留精确 AgentVersion 与 ReleaseRevision。

本次实现保留现有 Rank Agent 表原有的文本标识符。其物理主键迁移归属于规划中的 Agent Registry 提取，届时所有引用会一起迁移，不增加原地兼容层。新建的对外服务表已经在 Deployment、DeploymentAlias 和 ReleaseRevision 之间使用内部整数外键。

## Verification

Repository 测试断言对外服务表使用整数主键和独立公开标识符。应用测试覆盖 UUIDv7 生成、`latest` 自动推进、精确版本解析不生成发布修订、过期 Compare-and-Swap 拒绝且不遗留孤立记录、仅追加回滚、发布证据要求，以及通过已提交 Alias 当前指针完成解析。

## Alternatives considered

**所有主键和外键都使用 UUID。**这样只有一种标识符，但相较 `BIGINT` 会让键宽翻倍；UUIDv4 还会随机写入索引。UUIDv7 改善了局部性，却不能消除每个外键和 Join 的额外成本。

**公开自增标识符。**数据库内部更紧凑，但会暴露存储顺序、增加跨环境导入难度，并把实现细节变成公开协议的一部分。

**通过 `ORDER BY revision DESC LIMIT 1` 解析别名。**适当索引可以支持该查询，但每个服务请求都会读取历史表。DeploymentAlias 是已提交的 O(1) 读取投影，ReleaseRevision 继续保留完整审计历史。

**更新一个可变版本字段。**实现更简单，却会破坏发布来源，并且无法把回滚表达为一项带有独立证据和操作者的新决定。

## Consequences

对外服务标识符存在两种表示，因此 Persistence Adapter 必须把公开引用转换成内部主键，绝不能序列化物理主键。Alias 读取成本稳定，历史决定仍可完整重建。晋级冲突会在流量切换前失败，已受理调用会冻结解析结果，不必重新读取已经移动的 Alias。[Responses 对外服务 MVP](../feature/2026-09-09-responses-serving-mvp.md)基于该注册表实现 HTTP 生命周期、策略执行与持久化记录；PostgreSQL Adapter 仍属于生命周期服务提案。

完整协议和交付计划记录在 [Agent 全生命周期服务提案](../../proposed/feature/2026-09-08-agent-lifecycle-serving.md)。
