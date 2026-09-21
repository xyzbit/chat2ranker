# Rank API 滚动更新案例

[English](README.md) | 中文

此无密钥案例在替换 Rank HTTP Pod 时，通过本地 Kubernetes 网关运行真实 DSH/Execution 请求。它需要[完整远程平台实验](../../deploy/k8s/README.md)、一个纯 HTTP API 副本和一个独立调度器，不调用付费模型供应商。

```sh
node rank/examples/api-rolling/verify.mjs
```

确定性模型先通过一条回归用例再发布。25 秒模型响应验证排空期间完成；45 秒响应超过 API 的 30 秒 HTTP 退出等待上限。两次滚动更新期间，持续的历史及 Cookie 身份读取必须全部返回 200。超长流必须在没有成功终态事件时结束，而原 Response 随后以一次 Execution 尝试完成，并支持持久 SSE 回放。登录在替换后保留，退出撤销共享会话，调度器 Pod 保持不变。

脚本对照 [snapshot.json](snapshot.json) 验证结果，在私有实验目录保留 ID 和时间，撤销自己的公开 Key，并删除模型容器及端口转发，不改动真实模型凭据。fixture 和发布记录通过 `rolling-` 前缀识别。此案例验证单机上 schema 兼容的 API 替换，不覆盖中控替换、迁移或节点故障高可用。
