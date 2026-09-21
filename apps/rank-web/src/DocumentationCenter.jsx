import { useEffect, useState } from "react";
import { Check, Copy } from "@phosphor-icons/react";
import { endpointExamples } from "./endpoint-guide.js";
import { ExampleDownloads } from "./ExampleDownloads.jsx";
import integrationGuide from "../../../rank/examples/responses-client/download/README.md?url";
import integrationGuideZh from "../../../rank/examples/responses-client/download/README.zh.md?url";

/** Copyable source example shared by the documentation topics. */
function Example({ children, t }) {
  const [state, setState] = useState("");
  return <div className="docs-example"><button title={t("copyExample")} aria-label={t("copyExample")} onClick={async () => {
    try { await navigator.clipboard.writeText(children); setState("copied"); }
    catch { setState("copyFailed"); }
  }}>{state === "copied" ? <Check size={16}/> : <Copy size={16}/>}</button><pre><code>{children}</code></pre>{state && <small role="status">{t(state)}</small>}</div>;
}

/** Addressable product and integration documentation, served locally with the application. */
export function DocumentationCenter({ locale, t }) {
  const [query, setQuery] = useState(() => location.hash.split("?")[1] || "");
  const params = new URLSearchParams(query);
  const topic = params.get("topic") || "overview";
  const endpoint = params.get("endpoint") || "support";
  const docsLink = id => `#/docs?topic=${id}&endpoint=${encodeURIComponent(endpoint)}`;
  useEffect(() => {
    const update = () => setQuery(location.hash.split("?")[1] || "");
    window.addEventListener("hashchange", update);
    return () => window.removeEventListener("hashchange", update);
  }, []);
  const en = locale === "en";
  const topics = [
    ["overview", en ? "AgentKiln overview" : "认识 AgentKiln"],
    ["quickstart", en ? "Quick integration" : "快速接入"],
    ["examples", en ? "Runnable examples" : "完整案例"],
    ["chat", en ? "Chat applications" : "接入聊天工具"],
    ["stream", en ? "Streaming" : "流式输出"],
    ["instructions", en ? "Additional instructions" : "追加指令"],
    ["tools", en ? "External tools" : "外部工具"],
    ["errors", en ? "Troubleshooting" : "错误排查"],
    ["feedback", en ? "Invocation feedback" : "调用排查与用例沉淀"],
  ];
  const active = topics.some(item => item[0] === topic) ? topic : "overview";
  const examples = endpointExamples(endpoint, location.origin);
  const text = (zh, english) => en ? english : zh;
  return <main className="documentation-page">
    <nav aria-label={t("documentation")}>{topics.map(([id, title]) => <a key={id} href={docsLink(id)} aria-current={active === id ? "page" : undefined}>{title}</a>)}</nav>
    <article><h1>{topics.find(item => item[0] === active)[1]}</h1>
      {active === "overview" && <>
        <p>{text("AgentKiln 将 Agent 配置、试用、评测和对外发布放在同一个平台，帮助你检查不同版本的效果，并把选定版本提供给应用调用。", "AgentKiln brings Agent configuration, trials, evaluation and publication together so you can compare versions and serve a chosen version to applications.")}</p>
        <h2>{text("从配置到调用", "From configuration to invocation")}</h2>
        <ol><li>{text("在设置中选择模型连接，AgentKiln 中控助手和评测 Judge 使用各自的系统模型。", "Choose model connections in Settings. The AgentKiln control assistant and evaluation Judge have their own system model bindings.")}</li>
        <li>{text("创建 Agent，配置系统指令、模型、工具和 Skills。每次保存新版本都保留不可变配置。", "Create an Agent with system instructions, a model, tools and Skills. Each new version preserves immutable configuration.")}</li>
        <li>{text("在 Agent 页面直接试用，或在评测 Tab 选择默认测试集。开发集和回归集是用途，任何 Dataset 都可使用。", "Try the Agent on its page, or choose default datasets under Evaluation. Development and regression are uses of generic Datasets.")}</li>
        <li>{text("点击去发布，创建端点并确认版本。默认先运行回归测试，通过后发布；关闭回归会记录为跳过。", "Choose Publish, create an endpoint and review the version. Regression runs by default before publication; disabling it records a skip.")}</li>
        <li>{text("在端点接入侧栏创建 API Key，复制示例。后续发布只切换新会话的版本，已有会话保持原版本。", "Create an API key in the endpoint integration drawer and copy an example. Later releases update new conversations; existing conversations retain their version.")}</li></ol>
        <p>{text("端点是对外访问的稳定入口。测试和生产可使用不同端点名称。AgentKiln 助手负责管理和评测；Agent 试用才是与被测 Agent 对话。", "An endpoint is a stable public access target. Use separate endpoint names for test and production. AgentKiln manages and evaluates Agents; Agent trials invoke the tested Agent.")}</p>
        <a className="inline-link" href="#/docs?topic=quickstart">{t("quickIntegration")}</a>
      </>}
      {active === "quickstart" && <>
        <ol><li>{text(`准备已发布的 Agent 端点。以下示例使用 ${endpoint}；从端点的接入侧栏打开文档可自动带入名称。`, `Prepare a published endpoint. These examples use ${endpoint}; opening documentation from an endpoint's Integration drawer fills its name.`)}</li><li>{text("点击接入，选择已有 API Key 并填写原值，或直接创建新 Key。新 Key 默认授权当前已有的全部端点与操作，可以在创建时缩小范围。", "Open Integration. Choose an existing API key and enter its original value, or create a new one. New keys default to all existing endpoints and operations; narrow permissions before creation as needed.")}</li><li>{text("在服务端设置 RANK_API_KEY 环境变量，运行下面的命令。接入侧栏在拿到密钥后可以复制完整可执行命令。", "Set RANK_API_KEY in the server environment, then run the command below. The integration drawer can copy a complete executable command after receiving a key.")}</li></ol>
        <Example t={t}>{examples.curl}</Example><h2>JavaScript</h2><Example t={t}>npm install openai</Example><Example t={t}>{examples.javascript}</Example>
        <p>{text("使用 Node.js 22.19 或更高版本。将 JavaScript 放在 main.mjs 中，在已设置 RANK_API_KEY 的终端运行 node main.mjs；成功时输出两轮正文。model 填端点名称，底层模型由 Agent 版本决定。", "Use Node.js 22.19 or newer. Put the JavaScript in main.mjs and run node main.mjs with RANK_API_KEY set. Success prints both replies. model is the endpoint name; the Agent version chooses the underlying model.")}</p>
        <a className="inline-link" href={docsLink("examples")}>{text("运行每日热点和聊天助手案例", "Run the headlines and chat examples")} ↗</a>
        <p>{text("API Key 仅在创建时返回明文。页面不会从数据库恢复旧 Key，也不将它保存在浏览器存储中。关闭侧栏前保存新 Key。不要把密钥放入前端、聊天或代码仓库。", "API key plaintext is returned only at creation. It cannot be recovered from the database and is not persisted in browser storage. Retain a new key before closing the drawer. Keep keys out of frontend code, chat and repositories.")}</p>
        <h2>{text("常见返回", "Common responses")}</h2><p>{text("401：密钥无效或已撤销；403：缺少端点或操作权限；409：端点未发布、续聊不是最新回复或存在并发；429：请求过快或并发已满。失败回复需要检查 error，不要只读取 output_text。", "401: invalid or revoked key. 403: missing endpoint or operation permission. 409: unpublished endpoint, stale continuation or concurrent writer. 429: rate or concurrency limit. Inspect error on failed responses instead of reading only output_text.")}</p>
        <p>{text("本地地址仅供本机使用。其他机器接入需提供可达的 HTTPS 地址，并仅暴露 /v1 数据接口；管理页面和 /api 控制接口需要独立认证。", "The loopback address is local to this machine. Remote clients need a reachable HTTPS address exposing /v1; management pages and /api require separate operator authentication.")}</p>
      </>}
      {active === "instructions" && <>
        <p>{text("instructions 是一次 Responses 请求的顶层字符串，用来补充当前任务要求。无需打开能力开关，不会保存成 Agent 的新配置。", "instructions is a top-level string on a Responses request that adds requirements for the current turn. It requires no capability toggle and does not create a new Agent configuration.")}</p>
        <Example t={t}>{examples.instructions}</Example>
        <h2>{text("模型实际收到什么", "What the model receives")}</h2>
        <p>{text("假设 Agent 的系统指令是「你是客服助手」。AgentKiln 会在它后面追加两个换行和请求的 instructions，形成一份系统提示词。下面是语义等效的消息，不是 AgentKiln 接口要求的请求格式；具体 Harness 的传输方式可能不同。", "If the Agent system prompt is 'You are a support assistant', AgentKiln appends two newlines and the request instructions to form one system prompt. The following messages illustrate the equivalent meaning, not AgentKiln request syntax; each harness may transport the prompt differently.")}</p>
        <Example t={t}>{JSON.stringify([{role:"system", content:"You are a support assistant.\n\nReply in Chinese, in no more than three sentences."},{role:"user",content:"Explain how a rainbow forms."}],null,2)}</Example>
        <p>{text("不是两条 instructions 消息。使用 previous_response_id 续聊时，历史对话会保留，但前一轮追加指令不会自动继承；需要持续生效时每轮传入。追加指令属于高优先级任务要求，应用应自行控制来源。", "These are not two instructions messages. previous_response_id retains conversation history, but previous request instructions are not automatically inherited; send them on each turn when needed. Additional instructions are high-priority task requirements, so the calling application controls their source.")}</p>
      </>}
      {active === "tools" && <>
        <p>{text("外部工具是接入方提供的 Function。Agent 决定是否调用并返回函数名与参数；业务应用执行工具，再把结果传回来。AgentKiln 不执行调用方代码，也不会因为声明工具就授予业务系统权限。", "External tools are functions supplied by the calling application. The Agent may return a function name and arguments; your application executes it and sends the result back. AgentKiln does not execute caller code or grant business permissions from a tool declaration.")}</p>
        <Example t={t}>{examples.tools}</Example>
        <p>{text("示例执行读取服务端时间的工具。带参数的业务工具需通过 parameters 声明 JSON Schema，并验证参数与用户权限，只调用明确注册的函数。续接使用相同 call_id，工具定义随会话继承，不在续接请求中替换；如果再次收到 function_call，继续执行工具并回传结果。", "The example reads server time. Business tools with arguments declare JSON Schema in parameters and validate arguments and user permissions before dispatching registered functions. Continue with the same call_id; tool definitions are inherited and cannot be replaced. If another function_call arrives, execute it and return the result again.")}</p>
        <h2>{text("需要人工审批的工具", "Tools requiring approval")}</h2><p>{text("工具定义可设置 require_approval: true。回复停在 requires_action 后，用拥有审批权限的 Key 调用 POST /v1/responses/{id}/approval。批准后由应用执行并提交结果；拒绝会取消回复。", "Set require_approval: true on a tool. When the response reaches requires_action, call POST /v1/responses/{id}/approval using a key with approval permission. After approval, execute the tool and return its result; rejection cancels the response.")}</p>
        <Example t={t}>{JSON.stringify({approved:true, reason:"Approved by the application user"}, null, 2)}</Example>
      </>}
      {active === "stream" && <>
        <p>{text("设置 stream: true 后，服务返回 SSE。读取 response.output_text.delta 增量展示文字，不能把每个事件都当作最终回复。", "With stream: true, the service returns SSE. Render response.output_text.delta incrementally rather than treating every event as a final answer.")}</p><Example t={t}>{examples.stream}</Example>
        <p>{text("response.completed 表示完成；response.failed 和 response.cancelled 表示失败或取消。保存已完成回复的 id，用于下一轮 previous_response_id。中断后可 GET /v1/responses/{id}?stream=true&starting_after=N 从已收到的序号继续，或用 Last-Event-ID。取消请求使用 POST /v1/responses/{id}/cancel，需要取消权限。", "response.completed indicates completion; response.failed and response.cancelled indicate failure or cancellation. Save the completed response id for the next previous_response_id. Resume interrupted streams with GET /v1/responses/{id}?stream=true&starting_after=N or Last-Event-ID. POST /v1/responses/{id}/cancel requires cancel permission.")}</p>
      </>}
      {active === "chat" && <>
        <p>{text("聊天工具需要支持 OpenAI Responses API，自定义 Base URL、API Key 和模型名称。仅支持 /chat/completions 的工具不能直接接入这个接口。", "A chat tool must support OpenAI Responses with a custom Base URL, API key and model name. Tools limited to /chat/completions cannot connect directly.")}</p>
        <dl className="detail-facts"><div><dt>Base URL</dt><dd><code>{examples.baseURL}</code></dd></div><div><dt>API Key</dt><dd>{text("你创建的调用密钥", "Your serving key")}</dd></div><div><dt>Model</dt><dd>{endpoint}</dd></div><div><dt>API</dt><dd>POST /v1/responses</dd></div></dl>
        <p>{text("model 使用实际端点名称。浏览器聊天前端应通过你自己的服务端调用 AgentKiln，密钥只在服务端读取；每个用户的新聊天独立保存最新 response.id，发送下一轮时传 previous_response_id。不要让不同用户共享同一个会话 ID。", "Use the actual endpoint name as model. Browser chat frontends should call AgentKiln through your server, keeping keys server-side. Store the latest response.id separately for each user conversation and pass previous_response_id on the next turn. Never share a conversation ID across users.")}</p>
        <a className="inline-link" href={docsLink("quickstart")}>{text("查看多轮对话示例", "View the multi-turn example")}</a>
        <p><a href={docsLink("examples")}>{text("运行浏览器聊天案例", "Run the browser chat example")} ↗</a></p>
      </>}
      {active === "examples" && <>
        <p>{text("两个独立应用通过公开 Responses API 接入同一个端点：每日热点只发起一次 AgentKiln 请求，聊天助手支持流式续聊和取消。", "Two applications use the public Responses API against one endpoint: a single-request daily briefing and streaming chat with continuation and cancellation.")}</p>
        <ExampleDownloads endpoint={endpoint} locale={locale}/>
        <ol><li>{text("准备一个已发布的真实模型 Agent 端点；在接入侧栏创建仅授权该端点的 API Key，勾选发起、读取和取消操作。", "Publish a real-model Agent endpoint. Create a key scoped to it with create, read and cancel operations.")}</li><li>{text("下载并解压案例，在解压目录执行 npm ci。复制 .env.example 为 .env，填写 RANK_API_KEY。地址和端点名称已带入，可按需修改。", "Download and extract an example, then run npm ci in its folder. Copy .env.example to .env and fill RANK_API_KEY. The address and endpoint are prefilled and can be edited.")}</li><li>{text("运行 npm start，打开终端输出的本地地址。修改 .env 后重启 demo。", "Run npm start and open its printed local URL. Restart after changing .env.")}</li></ol>
        <Example t={t}>{"# macOS / Linux; Windows PowerShell: Copy-Item .env.example .env\nnpm ci\ncp .env.example .env\n# Fill RANK_API_KEY in .env\nnpm start"}</Example>
        <h2>{text("每日热点", "Daily headlines")}</h2><p>{text("应用采集 HN 最近 24 小时的标题、时间和来源，再调用一次 Agent 生成五条中文摘要。来源失败时不调用模型；摘要仅依据标题，不是全网热搜或全文核查。也可独立运行 npm run news。", "The app collects HN titles, timestamps and sources from the last 24 hours, then makes one Agent call for five Chinese summaries. Source failures stop before model invocation. This is title-based community activity, not global trends or full-article verification. Run npm run news for the CLI version.")}</p>
        <h2>{text("聊天助手", "Chat assistant")}</h2><p>{text("先提供姓名和偏好，再追问验证记忆。浏览器不保存密钥；服务端按访客保存最新 Response ID。刷新保留当前进程的会话，重启清空 demo 会话。停止会调用 AgentKiln 取消接口，之后需要新建对话。", "Provide a name and preference, then ask a follow-up to verify recall. Keys stay server-side, with a Response ID per visitor. Reload retains the current process session; restart clears it. Stop calls AgentKiln cancellation, after which a new conversation is required.")}</p>
        <h2>{text("验收", "Acceptance")}</h2><Example t={t}>{"npm test\nnpm run verify:demos"}</Example><p>{text("真实验收会调用模型并可能产生费用，验证热点、连续对话、流式结果、版本固定和取消。缺少配置或调用失败会报错，不会跳过。", "Live acceptance invokes the model and may incur charges, checking headlines, continuation, streaming, version pinning and cancellation. Missing configuration or failed calls report errors instead of skipping.")}</p>
        <a href={en ? integrationGuide : integrationGuideZh} target="_blank" rel="noreferrer">{text("纯文本接入教程（Markdown）", "Plain-text integration tutorial (Markdown)")} ↗</a>
        <p><a href={docsLink("errors")}>{text("调用失败时如何排查", "Troubleshoot a failed invocation")} ↗</a></p>
      </>}
      {active === "errors" && <>
        <p>{text("先区分请求被拒绝和 Agent 执行失败。401、权限不足等接收前错误通常没有 Response ID，也不会产生调用记录；保留 HTTP 状态和 error.code 即可定位。请求已接收时，保留 response.id，再进入调用记录搜索。不要在排查材料中附上 API Key。", "Separate rejected requests from Agent execution failures. Pre-acceptance errors such as 401 or insufficient permissions usually have no Response ID or invocation record; retain the HTTP status and error.code. Once accepted, retain response.id and search invocation records. Never include the API key in diagnostics.")}</p>
        <dl>
          <dt>401</dt><dd>{text("确认使用 AgentKiln 的 API Key，而非模型供应商密钥；修改 .env 后重启。已撤销的 Key 需要重新创建。", "Use a AgentKiln key, not a provider key, and restart after editing .env. Replace revoked keys.")}</dd>
          <dt>403 / 404</dt><dd>{text("检查 Key 是否拥有该端点及本次操作权限。不可见资源可能返回 404。聊天停止需要取消权限，恢复读取需要读取权限。", "Check endpoint and operation grants; invisible resources can return 404. Stopping needs cancel permission, while retrieval needs read permission.")}</dd>
          <dt>409</dt><dd>{text("检查端点是否已有发布版本。续聊时使用最新完成的 Response ID，同一会话等待上一轮结束；失败或取消后新建对话。", "Check that the endpoint has a published version. Continue from the latest completed Response ID, with only one running turn per conversation. Start a new conversation after failure or cancellation.")}</dd>
          <dt>429</dt><dd>{text("检查端点限流和并发策略，等待后重试。示例关闭 SDK 自动重试，避免不明原因重复请求。", "Check endpoint rate and concurrency limits and retry after waiting. SDK automatic retries are disabled in the examples.")}</dd>
          <dt>{text("HTTP 200，但没有完整结果", "HTTP 200 without a complete result")}</dt><dd>{text("同步请求必须检查 status，流式请求必须收到 response.completed。response.failed、response.cancelled 或没有完成事件均不能当作成功。用 Response ID 检查冻结版本、模型连接和执行错误。", "Check status for synchronous calls and require response.completed for streams. response.failed, response.cancelled and missing completion events are not success. Inspect the frozen version, model connection and execution error by Response ID.")}</dd>
          <dt>{text("流式连接中断", "Interrupted stream")}</dt><dd>{text("断开连接不等于取消执行。Demo 中先刷新查看已保存状态；自行接入时通过 GET /v1/responses/{id} 查询，或携带最后的 sequence_number 恢复 SSE。只有确认完成后才能续聊，不要直接重发创建请求。", "Disconnection does not cancel execution. Reload the demo to see saved state. Custom clients can GET /v1/responses/{id} or resume SSE using the last sequence_number. Continue only after completion; do not blindly resend a creation request.")}</dd>
          <dt>{text("热点来源失败", "News source failure")}</dt><dd>{text("检查 hn.algolia.com 是否可达。此时尚未调用 AgentKiln，不会有 Response ID；不要使用模型记忆代替今日资料。", "Check reachability of hn.algolia.com. AgentKiln has not been invoked, so there is no Response ID. Do not replace current source data with model memory.")}</dd>
        </dl>
        <a className="inline-link" href="#/responses">{text("打开调用记录", "Open invocation records")} ↗</a>
      </>}
      {active === "feedback" && <>
        <ol><li>{text("在调用记录中输入 Response ID，或从应用的调用记录链接进入；详情链接可复制、刷新。", "Search invocation records by Response ID or follow the application link. Detail URLs can be copied and reloaded.")}</li><li>{text("检查冻结的 Agent 版本、失败原因、输入输出、Token 与费用；请求配置包含追加指令和外部工具，上一轮和整个会话用于排查上下文。", "Inspect the frozen Agent version, failure, input/output, tokens and cost. Request configuration includes additional instructions and caller tools. Previous-turn and conversation links expose context.")}</li><li>{text("点击加入数据集，选择现有数据集或新建，编辑脱敏输入并填写期望结果。多轮历史和工具结果需整理成独立用例所需的文本；实际输出不会自动作为正确答案。", "Choose Add to Dataset, select an existing dataset or create one, redact input and write the expected result. Summarize history and tool results into standalone text; actual output is never assumed to be correct.")}</li><li>{text("确认后保存。现有数据集会新增版本并保留来源；旧 Run 不受影响，重复来源会拒绝。点击去评测，带出同一 Agent 版本和新数据集，再确认运行。", "Save after review. Existing datasets gain a new version with provenance; old Runs stay unchanged and duplicate sources are rejected. Evaluate carries the same Agent version and new dataset into run preparation.")}</li></ol>
        <p>{text("数据集保持通用。将该数据集设为 Agent 的开发或回归默认值后，后续评测和发布可自动带出；单次运行仍可替换。", "Datasets remain generic. Set one as the Agent's development or regression default to prefill later evaluations and releases; individual runs can override it.")}</p>
      </>}
    </article>
  </main>;
}
