# 本地 Rank 接入案例

[English](README.md) | 中文

本教程通过独立 Node.js 客户端，使用官方 OpenAI SDK 调用已发布的 Rank Agent。默认案例先让 Agent 记住 `RANK-2048`，再流式追问编号，查询已保存的回复，并断言记忆和实际 Agent 版本一致。案例会调用模型两次，可能产生 Provider 费用。

## 前提

需要 Node.js 22.19+，以及正在运行且支持 `/v1/responses`、Deployment 和调用密钥的 Rank 版本。仅提供评测的版本没有这些接口。在 Rank 中验证模型连接，将使用真实模型的 Agent 发布到某个 Deployment 的 `production` 别名。案例不会启动或修改 Agent 运行环境，也不会执行发布。

## 本地运行

在本目录执行：

```sh
npm ci
RANK_MODEL=acceptance-mtu67jzo@production npm run verify:local
```

将部署名称换成自己的名称。本地验证通过仅限回环地址的管理 API 创建临时密钥，只授予该 Deployment 的创建和读取权限，运行客户端，并在 `finally` 中撤销密钥。密钥不会被打印或保存，已接受的 Response 记录会保留在 Rank。若进程被强制结束或撤销失败，请在 Rank 中撤销名为 `Local Responses example` 的密钥。

成功输出包含两轮回复、实际模型引用和 Response ID、`PASS`，以及临时密钥已撤销的提示。请求或断言失败时退出码为 1，错误不代表接入成功。

## 接入自己的应用

在 Rank 创建有权访问目标 Deployment 且具有 `responses:create`、`responses:read` 权限的调用密钥。参照 [.env.example](.env.example) 的字段配置本地 `.env`：

```dotenv
RANK_BASE_URL=http://127.0.0.1:8787/v1
RANK_MODEL=your-deployment@production
RANK_API_KEY=your-rank-serving-key
```

密钥放在应用服务端环境中。这里使用 Rank 调用密钥，不是 DeepSeek 等 Provider 的密钥。Git 已忽略 `.env`。

```sh
npm start
npm start -- "Give me a one-sentence introduction."
```

不带问题时运行两轮案例；带问题时流式开启一轮新对话。[client.mjs](client.mjs) 是公开 API 接入代码；[verify-local.mjs](verify-local.mjs) 仅用于本地管理和验证，外部应用不需要它。SDK 已关闭自动重试，避免失败时静默增加请求。案例仅使用声明支持的 Responses 子集，不代表完整兼容所有 OpenAI API。

## 排查

连接被拒绝表示配置地址上没有 Rank API 监听。Serving 路由返回 `404` 表示版本不支持 Serving；资源 `404` 也可能来自部署名称错误或权限不足。`401` 表示密钥缺失、已撤销或无效。创建密钥时返回 `500` 属于 Rank 服务或存储故障，应先查看服务日志，恢复工作目录和数据库再重试。健康接口返回 `200` 本身不能证明写入和模型执行可用。
