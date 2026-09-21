# 共享模型限流案例

[English](README.md) | 中文

此无密钥案例启动打包的 Execution API、两个 Worker 进程和真实 DSH，使用本地 HTTP 供应商与独立 PostgreSQL schema。它验证跨连接共享 RPM、独立 TPM 阻塞、取消、重放、重启后的额度保留，以及请求产物不保存临时凭据。

## 运行

先执行 `pnpm run build:lib:host` 构建 DSH，通过 `EXECUTION_TEST_DATABASE_URL` 提供可丢弃的 PostgreSQL 数据库地址，再从仓库根目录运行 `pnpm run rank:test:model-quota`。测试使用 Go 1.26.0 构建二进制，并比对 `snapshot.json`，不使用供应商密钥。退出时清理子进程和私有临时文件；打印的 `quota_<hex>` schema 保留供检查。完成后仅对该 schema 执行 `DROP SCHEMA <printed_schema> CASCADE`；不要使用生产数据库。

## 配置共享额度

此能力使用 PostgreSQL。将 `EXECUTION_MODEL_QUOTA_URL` 指向运行时可访问的 `/v1/model-permits`，为 API 和 Worker 提供单独的 `EXECUTION_MODEL_QUOTA_KEY`，至少 32 个字符。它签发仅供一次运行使用的回调身份，不要向运行时发送服务令牌。`-model-quota-timeout` 默认 5 秒，`-model-permit-validity` 默认一秒。使用与服务相同的数据库和 schema 执行 `executiond -configure-model-quota quota.json`：

```json
{"id":"provider-account","connections":["connection-a","connection-b"],"requestsPerMinute":60,"tokensPerMinute":100000,"maxOutputTokens":512}
```

连接 ID 必须是真实且已验证的连接。零表示关闭某一种速率限制，至少保留一种正数限制。共用同一个供应商账号的连接应放入同一组。修改速率保留已有用量；修改成员需要先排空排队、活跃及等待清理的工作，并等待记账窗口结束。输出上限在准入时冻结；配置竞争返回 HTTP 409，可使用相同幂等键再次提交。

许可在多个 Worker 间串行核算实际 DSH 流请求尝试。记账期限为 60 秒加许可允许的最大派发间隔。Token 先保守估算，再根据正数用量报告调整，不代表供应商精确计费或费用上限。用量缺失时保留预留。启动前已耗尽的额度不计入扩容需求，但此检查不预留首个请求：运行中的 Harness 仍可能等待并占用槽位与固定寿命。不支持该能力的适配器和 SQLite 拒绝携带额度计划的工作。

minikube 远程运行时及有界 DeepSeek 验证见[部署实验](../../deploy/k8s/README.md)。
