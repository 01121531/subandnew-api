# SubAndNew API 微信 AI 智能助手改造方案

> 状态：设计提案
> 更新时间：2026-08-28
> 目标：在不破坏现有控制平面边界的前提下，通过微信官方 iLink/ClawBot 通道，用自然语言安全、准确、低延迟地查询多实例数据，并逐步支持受控运维。

## 0. 执行摘要

当前项目已经不是 AI 网关，而是一个成熟的多实例控制平面。它已经具备实例纳管、统一观测、实时指标、账号与使用记录、账单/指标告警、后台任务、RBAC、操作预览、幂等、审计和配置治理。微信助手不应复制这些能力，也不应绕过它们直接请求远端实例；它应该成为现有控制平面的一个受控交互入口。

推荐目标形态：

```text
微信用户
   │
   ▼
腾讯 iLink Bot API
   │  长轮询 / context_token / typing / media
   ▼
WeChat Channel Adapter
   │  inbox 去重、游标事务、账号租约、outbox
   ▼
Assistant Orchestrator
   ├─ 身份绑定与 RBAC
   ├─ 单 Agent 工具调用
   ├─ 会话、偏好、取消、超时
   ├─ 工具策略与写操作确认
   └─ Trace / Audit / Eval
   │
   ▼
受控 Tool Registry
   ├─ managedinstance 查询服务
   ├─ dashboard / realtime / usage / account
   ├─ billing / metric alerts
   ├─ system tasks / exports
   └─ PlanOperation → 人工确认 → ExecuteOperation
```

核心决策：

1. 微信协议以腾讯官方 `Tencent/openclaw-weixin` 为权威基准，Go 实现通过自有 `WeChatChannel` 接口隔离，Phase 0 对候选 SDK 做契约验证后再锁定版本。
2. 默认采用当前 Go 单体内的模块化实现，只在吞吐或故障隔离确有必要时拆成独立 assistant worker。
3. MVP 只开放只读工具；任何远端写操作都不能由模型直接执行。
4. 助手以绑定后的控制平面用户身份执行，每次工具调用都重新做 RBAC 与实例范围校验，微信身份本身不授予权限。
5. 生产 MVP 使用单个受控工具调用 Agent，不引入多 Agent 自主协作。当前问题域边界明确，单 Agent 更快、更便宜、更容易审计和评测。
6. 不在第一阶段建设通用 RAG、向量数据库、任意 MCP、任意 URL、SQL、Shell 或浏览器工具。
7. 回答中的业务数字必须来自工具结果，并显示数据范围、采集时间、实例来源和新鲜度；模型不能凭上下文补数字。

## 1. 当前项目审计

### 1.1 产品边界

`README.md` 已明确本项目是多实例控制平面，业务请求仍由数据平面处理。当前纳管 New API、HUICHUAN-AI、Sub2API、Conductor、Claude Gateway 和通用健康端点。

助手调用模型是控制平面自身的交互能力，不代表恢复已删除的本地 Relay、Channel 池或公共 AI 网关。它必须使用专用模型凭据，不能复用远端实例的管理员凭据作为推理凭据。

### 1.2 可以直接复用的能力

| 现有能力 | 代码位置 | 助手用途 |
| --- | --- | --- |
| 多实例适配器 | `service/managedinstance/adapter.go` | 用统一工具查询不同数据平面 |
| 汇总、资源、趋势 | `service/managedinstance/observability.go` | 回答请求量、Token、费用、错误率、延迟、账号状态 |
| 实时指标 | `service/managedinstance/managed_realtime.go` | 回答 RPM、容量、成功率、并发、账号可用性 |
| 使用记录与导出 | `service/managedinstance/usage_records.go` | 自然语言筛选、汇总与文件导出 |
| Dashboard 快照 | `service/managed_dashboard.go` | 优先读快照，降低延迟与远端压力 |
| 账单/指标告警 | `service/billingalert`、`service/metricalert` | 回答异常、阈值和恢复状态 |
| 持久任务和租约 | `service/system_task.go`、`model/system_task.go` | 持久化 Agent run、按会话串行、失败恢复 |
| 操作预览与幂等 | `service/managedinstance/operation.go` | 写操作先 plan，再确认与执行 |
| RBAC | `service/authz` | 工具级授权，不信任模型决策 |
| 管理操作审计 | `middleware/audit.go` | 扩展为 channel/run/tool 审计 |
| AES-256-GCM 凭据加密 | `service/managedinstance/credential.go` | 提取为通用 secret cipher 后保护 Bot/模型凭据 |
| React 管理端 | `web/default` | 扫码、模型、绑定、会话、审计、评测和 Playground |

### 1.3 已有微信能力的真实范围

当前代码中的微信配置属于登录/OAuth：`WeChatAuthEnabled`、`WeChatServerAddress`、`WeChatServerToken` 和登录二维码。它不能承担机器人长轮询、消息收发、会话、Agent、工具调用或回复投递。

因此应新增独立 `assistant` 与 `channel/wechat` 领域，不应扩展现有 OAuth handler 来承载机器人逻辑。

### 1.4 当前质量基线

2026-08-28 已验证：

```text
go test ./... -count=1      通过
go vet ./...                通过
bun run typecheck           通过
bun run lint                通过
bun run build               通过
```

这说明适合做增量改造，无需推倒重构。

### 1.5 必须先处理的安全事实

`service/managedinstance/connector.go` 当前 `ConnectorPolicyFromEnvironment` 实际允许 `0.0.0.0/0,::/0`，HTTP client 允许最多 5 次重定向。`README.md` 前部仍写有“限制私网、回环、重定向”，但后部已说明默认不限制目标 IP、主机或端口。

这在受信任内网的管理员控制台中尚可作为显式部署选择，但不能扩大到模型可自由选择的目标。助手必须满足：

- 不提供任意 URL/fetch 工具；
- 不提供任意 SQL、Shell、文件系统或浏览器工具；
- 不允许模型构造远端管理 API path；
- 每个工具只能调用固定、类型化的应用服务；
- 任意 MCP 只能在后续通过服务器与工具双白名单接入；
- 模型和微信 HTTP client 使用独立 egress policy，不能复用宽权限实例连接器。

### 1.6 平台级上线前置加固

仓库审计还发现以下问题。它们不要求在 PoC 前全部重构，但必须进入生产上线门禁：

1. 当前认证中间件主要信任 30 天签名 Cookie 中的 user id、role 和 status，用户被禁用或降权后，旧 Cookie 可能不能立即反映变化。助手不能继承或转换浏览器 Cookie；每条微信消息、每次工具调用和每次 Web approval 都要重新查用户状态与当前 RBAC。
2. 当前 RBAC 是 resource/action 级，没有实例级 object scope。`assistant_identities.allowed_instance_scope` 是额外的 ABAC 约束，不得只依赖全局 `managed_instance.*` 权限。
3. 当前凭据解密只接受一个当前 key version，直接更换主密钥会导致旧密文不可读。正式引入 Bot 和模型密钥前先建设 keyring：多版本解密、单一当前版本加密、后台重加密和轮换进度。
4. Compose 示例包含可预测的 Session/数据库占位密码，程序未完整拒绝所有示例值。生产模式应校验已知占位符、最低长度和熵，不满足时 fail closed。
5. 首次 setup 是未认证入口。生产首次启动只能在受信任网络中进行，或增加一次性 bootstrap token。
6. Gin 限流与审计依赖 Client IP，应显式配置可信代理；容器增加应用 health/readiness、非 root 用户、备份恢复说明和镜像/SBOM 扫描。
7. 现有 CI/Release 尚未把全量 Go test、vet、前端 typecheck/lint/build 作为统一发布门禁。Assistant 引入后必须补齐，并加入协议契约、安全 eval 和 secret scan。

## 2. GitHub 项目调研与选型

### 2.1 微信通道

| 项目 | 结论 | 采用方式 |
| --- | --- | --- |
| [Tencent/openclaw-weixin](https://github.com/Tencent/openclaw-weixin) | 腾讯官方、MIT；协议权威度最高；支持扫码、多账号、长轮询、typing、文本与媒体；依赖 OpenClaw/Node | 作为协议金标准、JSON fixture 与兼容测试 oracle，不默认整套引入 |
| [corespeed-io/wechatbot](https://github.com/corespeed-io/wechatbot) | 社区多语言 SDK；Go 1.22+；覆盖凭据/游标/context token、媒体、typing、恢复、多账号；生态和提交量在 Go 候选中较强 | Phase 0 第一候选，必须代码审计、锁定 commit，并由项目自身补 inbox/outbox、租约和授权 |
| [openilink/openilink-sdk-go](https://github.com/openilink/openilink-sdk-go) | 纯标准库、接口小、易审计；支持 QR、动态长轮询、媒体 | 作为轻量备选和协议实现参考；不能原样承担可靠消息总线 |
| [lib-x/ilink](https://github.com/lib-x/ilink) | stdlib 风格清晰，显式 context token，体量小 | 适合参考 wire/types；生产运行时能力需自行补齐 |
| [the-yex/wechat-ilink-sdk](https://github.com/the-yex/wechat-ilink-sdk) | HTTP client/token store 可注入，生命周期和退避设计较完整；生态较小 | 作为第二实现对照，重点验证 context token 持久化和许可证元数据 |
| [jeffkit/ilink-hub](https://github.com/jeffkit/ilink-hub) | 单账号到多 Agent backend 的透明代理；持久映射、有界队列、Prometheus | 只有一个 Bot 确实要共享给多个独立 Agent 时采用；当前不需要增加 Rust sidecar |
| [OpenILink Hub](https://github.com/openilink/openilink-hub) | Web 控制台和 App 市场完整，但与现有用户、权限、模型和审计高度重叠 | 仅参考控制台和快速 PoC，不进入核心信任边界 |
| 非官方 Wechaty puppet / WeChatFerry / wxhelper / Gewechat | 依赖逆向协议、Hook、特定客户端或灰色运行边界 | 不作为生产主链路 |

个人微信 iLink 当前按以下边界设计：

- 私聊优先，不承诺群聊；
- 通道声明为非真正 token 流式，不能刷屏模拟流式；
- 通过 typing、一次进度提示和尽量单条最终回复改善等待感；
- `context_token`、重复投递、session 失效等按最坏情况防御，但不把社区观察写成腾讯 SLA；
- 正式上线前以微信客户端实际展示条款和腾讯政策为准。

如产品需要企业群聊、真正流式 Markdown、模板卡片和长期主动消息，新增官方 [企业微信智能机器人 SDK](https://github.com/WecomTeam/aibot-node-sdk) adapter，不用个人 iLink 勉强模拟。

### 2.2 Agent 与互操作

| 项目 | 可借鉴内容 | 结论 |
| --- | --- | --- |
| [CloudWeGo Eino](https://github.com/cloudwego/eino) | Go ChatModel、Tool、Graph、ADK、interrupt/resume | 可在 Phase 0 做框架 spike；只采用 ChatModel + Tool loop，不上 DeepAgent |
| [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) | 官方 Go MCP client/server | 后续把只读能力暴露给其他 Agent；不用于进程内替代 Go 调用 |
| [LangGraph](https://github.com/langchain-ai/langgraph) | 持久 checkpoint、interrupt/resume、长任务状态机 | 借鉴运行状态、恢复与人工确认思想，不为此引入 Python 服务 |
| [OpenTelemetry GenAI 语义约定](https://github.com/open-telemetry/semantic-conventions-genai) | inference、tool、retrieval、memory span | 作为 trace 命名与属性基线，内容默认不采集 |

## 3. 产品体验设计

### 3.1 首次使用

1. 管理员在 Web 的“AI 助手 → 微信通道”点击“连接微信”。
2. 后端创建短期 QR 登录会话，页面显示二维码、已扫码、待确认、连接成功或需重新认证。
3. 微信用户首次发消息时，Bot 只返回绑定指引，不泄露任何业务信息。
4. 用户在已登录 Web 控制台生成 6 位一次性绑定码，5 分钟内发送给 Bot。
5. 后端把 `channel + account + external_user_id` 绑定到控制平面 user，记录审批人、时间和允许实例范围。
6. Bot 返回当前身份、可查询范围、示例问题和 `/帮助`。

不自动复用现有 `wechat_id`：OAuth 微信身份和 iLink sender id 是否同一命名空间不能被假定。

### 3.2 推荐提问体验

用户可以直接问：

- “今天所有实例花了多少钱？”
- “现在 RPM 最高的三个实例是谁？”
- “上海时区昨天 Claude 的请求量和成功率。”
- “哪些账号快到期或被限流？”
- “列出过去 2 小时的异常和恢复情况。”
- “把 A 实例本周的使用记录导出给我。”
- “为什么总费用比昨天高？”

最终回复模板：

```text
结论：今天 8 个实例累计 $123.45，比昨天同期高 18.2%。

主要变化
1. prod-a：$52.10（+31%）
2. gateway-c：$28.40（+12%）
3. sub2-b：$17.80（-4%）

异常：prod-a 10:20–10:34 成功率降至 93.1%，现已恢复。

数据范围：2026-08-28 00:00–14:32，Asia/Shanghai
采集时间：14:32:08；8/8 实例成功，0 个陈旧
```

### 3.3 等待、进度和失败

- 收到消息后目标 300ms 内发送 typing；
- 1.5–2 秒仍未完成时最多发一次有意义的进度，如“正在汇总 8 个实例…”；
- 不逐 token 刷屏；最终尽量合并为一条消息；
- 超长内容按自然段分片，并带 `1/3`；
- 支持 `/取消`、`/重试`、`/继续`、`/清空上下文`、`/忘记偏好`、`/帮助`；
- 数据源失败时明确区分“没有数据”“数据陈旧”“无权限”“上游失败”；
- 部分实例失败时返回成功部分与失败清单，不把部分结果伪装成完整结果；
- 每个数字都可在 run trace 中追溯到工具输出字段。

### 3.4 时间与上下文

- 默认时区 `Asia/Shanghai`，回复始终显示时区；
- “今天/昨天/本周”用确定性代码解析，不交给模型自由计算；
- 时间范围、实例名或指标含糊且会显著改变结果时，只追问一个最关键问题；
- 允许保存结构化偏好：默认实例、环境、时区、默认时间范围、回复精简度；
- 不自动把任意聊天内容写成长时记忆。

### 3.5 主动通知

个人 iLink 的主动回复依赖有效 `context_token`，不能承诺永久主动推送。设计为：

- 告警通知仅在有效会话窗口内尽力发送；
- context 不可用时转邮件/Web 通知，并标记微信待恢复；
- 用户再次发任意消息后恢复微信回复能力；
- 企业级长期主动通知优先使用企业微信通道。

## 4. 目标架构与模块边界

### 4.1 后端目录建议

```text
model/
  assistant_channel.go
  assistant_identity.go
  assistant_conversation.go
  assistant_message.go
  assistant_run.go
  assistant_delivery.go
  assistant_model_profile.go

service/assistant/
  service.go                 # 用例入口
  identity.go                # 绑定、撤销、实例范围
  conversation.go            # 会话、消息、偏好
  runner.go                  # Agent run 状态机
  model_client.go            # 模型抽象与 fallback
  prompt.go                  # 版本化 prompt
  tool_registry.go           # 类型化工具注册
  policy.go                  # RBAC、风险等级、预算
  confirmation.go            # 确认 nonce 与 step-up
  memory.go                  # 结构化偏好与摘要
  audit.go                   # run/tool/channel 审计
  eval.go                    # 离线评测
  delivery.go               # inbox/outbox

service/assistant/channel/
  channel.go                 # 通道接口
  wechatilink/
    adapter.go
    login.go
    poller.go
    sender.go
    media.go
    health.go
  wecom/                     # 可选 Phase 4

service/assistant/tools/
  instances.go
  dashboard.go
  realtime.go
  usage.go
  accounts.go
  alerts.go
  exports.go
  operations.go

controller/
  assistant.go

router/
  assistant-router.go

web/default/src/features/assistant/
  overview/
  channels/
  models/
  bindings/
  conversations/
  audits/
  evals/
  playground/
```

### 4.2 通道接口

```go
type Channel interface {
    Type() string
    Login(ctx context.Context, channelID int64) (*LoginSession, error)
    Run(ctx context.Context, channelID int64, handler InboundHandler) error
    Send(ctx context.Context, delivery Delivery) (*SendResult, error)
    SetTyping(ctx context.Context, conversation ConversationRef, active bool) error
    Health(ctx context.Context, channelID int64) Health
}
```

业务层只能依赖此接口，不能依赖某个 SDK 的文件存储、全局状态或内部重试语义。

### 4.3 Agent Runner

MVP 是有界单 Agent 工具循环：

```text
Normalize input
→ deterministic command/time parser
→ load identity + permissions + short context + preferences
→ model selects typed tools
→ policy validates every call
→ execute read tools
→ model composes grounded answer
→ answer validator checks citations/freshness/size
→ outbox
```

硬限制建议：

- 每轮最多 6 次工具调用；
- 单轮默认 deadline 30 秒，可配置上限 60 秒；
- 同一会话同一时间只运行一轮；
- 每用户与每通道并发、RPM 和日预算限制；
- 输出长度与微信分片数量限制；
- 模型返回未知工具、非法参数或超预算时由代码拒绝，不做“尽力执行”。

### 4.4 模型提供商

新增 `AssistantModelClient` 抽象，第一阶段支持 OpenAI-compatible tool calling。模型配置包含：

- profile name；
- base URL；
- encrypted API key；
- model；
- timeout；
- max output；
- max tool steps；
- primary/fallback；
- health、最近延迟、错误率和熔断状态。

模型凭据必须是专用低权限推理凭据。即使 base URL 指向一个已纳管数据平面，也不能复用 `ManagedInstanceCredential` 中的管理员令牌。

Eino 只通过内部接口接入。若 Phase 0 发现直接实现一个可控 tool loop 更小、更稳定，可以不引入完整 ADK；成功标准是行为与可观测性，不是使用某个框架。

## 5. 工具设计

### 5.1 MVP 只读工具

| 工具 | 输入 | 输出 | 依赖权限 |
| --- | --- | --- | --- |
| `list_instances` | environment/kind/status/labels | 脱敏实例列表 | `managed_instance.view` |
| `get_instance_health` | instance_ids | 健康、版本、延迟、采集时间 | `managed_instance.view` |
| `get_dashboard_summary` | instance_ids/range/timezone | 请求、Token、费用、错误率、延迟、趋势 | `managed_instance.view` |
| `get_realtime_metrics` | instance_ids | RPM、容量、成功率、并发、账号可用性 | `managed_instance.usage_view` |
| `get_rpm_history` | instance_ids/range/bucket | 时序数据与峰值 | `managed_instance.usage_view` |
| `get_usage_summary` | instance_id/range/filters | 请求、Token、金额 | `managed_instance.usage_view` |
| `list_accounts` | instance_id/status/expiry/limit/cursor | 脱敏账号数据 | `managed_instance.usage_view` |
| `get_account_output` | instance_id/range | 账号产出和利用率 | `managed_instance.usage_view` |
| `list_alert_events` | scope/type/status/range | 告警、恢复、数据失败 | 对应 alert view 权限 |
| `create_usage_export` | instance/range/filters/format | 持久任务 ID | `managed_instance.usage_view` |
| `get_export_status` | task_id | 进度、错误、文件元数据 | 创建者或 Root |

工具输出必须是小而稳定的 DTO，不把远端原始 JSON、凭据、内部错误栈或任意日志直接送入模型。

### 5.2 风险等级

| 等级 | 类型 | 策略 |
| --- | --- | --- |
| R0 | 快照/缓存只读 | 有权限即可自动执行 |
| R1 | 主动刷新、生成导出 | 限流、预算和审计；可自动执行 |
| R2 | 测试资源、启停单资源 | 先 plan，微信内一次性确认码，2 分钟失效 |
| R3 | 批量写、配置应用、删除、凭据操作 | 只生成 plan；必须跳转 Web 并通过 session + 2FA/Passkey 确认 |

### 5.3 写操作闭环

```text
用户意图
→ 模型调用 plan 工具
→ 后端重新鉴权并生成现有 Operation Plan
→ 返回目标、影响、风险、ETag、过期时间
→ R2: 用户回复精确确认 nonce
   R3: 用户打开一次性 Web approval link 完成 step-up
→ 后端验证 actor、conversation、plan hash、nonce、TTL、权限、ETag
→ ExecuteOperation(idempotency_key)
→ 状态与审计回传微信
```

模型永远拿不到 `ExecuteOperation` 的裸工具。执行入口只能由 confirmation service 调用。

现有 operation execute 流程还需补一条不变量：执行 actor 必须等于 plan actor，或满足明确记录的 Root 接管规则。Assistant confirmation 必须绑定 user、conversation、plan hash、operation id 和 TTL，不能只凭 operation id 与幂等键执行。

## 6. 数据模型

### 6.1 核心表

| 表 | 关键字段与约束 |
| --- | --- |
| `assistant_model_profiles` | provider/base_url/model/encrypted_secret/key_version/status/fallback_id；密钥不回传 |
| `assistant_channels` | type/account_id/status/enabled/config/last_seen/reauth_reason；`type+account_id` 唯一 |
| `assistant_channel_secrets` | channel_id/ciphertext/key_version/fingerprint；Bot token 与 context 关联数据加密 |
| `assistant_identities` | channel_id/external_user_id/user_id/status/allowed_instance_scope/bound_by；唯一映射 |
| `assistant_inbox` | channel/account/external_message_id/seq/payload/status/cursor；消息唯一键防重 |
| `assistant_conversations` | channel/account/peer/user/status/summary/last_message_at；按 account+channel+peer 隔离 |
| `assistant_messages` | conversation/role/content_json/status/external_id/created_at；原始媒体不内嵌 |
| `assistant_runs` | conversation/trigger_message/model/prompt_version/status/deadline/token/cost/error/trace_id |
| `assistant_tool_calls` | run/tool/args_redacted/result_digest/status/permission/risk/latency |
| `assistant_confirmations` | run/plan_hash/nonce_digest/risk/expires_at/confirmed_by/status |
| `assistant_outbox` | channel/conversation/reply_key/payload/status/attempt/next_attempt/remote_result；`reply_key` 唯一 |
| `assistant_preferences` | user_id/timezone/default_instances/verbosity；仅结构化白名单字段 |
| `assistant_feedback` | run/user/rating/category/comment；用于评测与回归 |

### 6.2 保留策略

默认建议：

- 原始聊天内容 30 天；
- 完整工具结果 7 天，之后只保留摘要、digest 和审计元数据；
- run/tool 审计 180 天；
- 安全与写操作审计按现有合规策略延长；
- 媒体临时对象 24 小时或发送成功后立即清理；
- 支持按用户“清空上下文”和管理员合规删除；
- Trace 默认不记录 system prompt、用户原文、工具完整结果和模型完整输出。

## 7. 微信可靠性设计

### 7.1 登录状态机

```text
UNBOUND
→ QR_ISSUED
→ SCANNED
→ VERIFY_REQUIRED（可选）
→ CONNECTED
→ DEGRADED
→ REAUTH_REQUIRED
```

二维码和验证码只展示在管理员页。Bot token、context token、AES key 不写日志，UI 只显示 fingerprint 或后 4 位。

### 7.2 单账号单 Poller

iLink 的同一账号不能让多个副本同时竞争 `getupdates`。每个账号使用数据库租约：

- lease key：`assistant:wechat:<channel_id>`；
- owner：node + random runner id；
- TTL + heartbeat；
- fencing token 防止旧 owner 恢复后继续提交；
- 丢失租约立即取消 long poll；
- SQLite 只支持单节点；生产多节点推荐 PostgreSQL。

### 7.3 事务 Inbox 与游标

不能采用“先保存新 cursor，再异步处理消息”的顺序，否则中途崩溃可能丢消息。建议：

1. 收到 batch；
2. 同一数据库事务中按 `account_id + message_id` 插入 inbox，并持久化新 cursor；
3. 重复键视为已接收，不再创建第二个 run；
4. 提交后唤醒 worker；
5. worker 按 conversation 顺序消费；
6. 失败保留 inbox，按分类重试或进入 dead-letter。

缺失稳定 message id 时，退化使用 seq/client_id/时间窗内容哈希，并记录低置信度去重。

### 7.4 会话内消息合并

用户可能连续发送多条短消息。采用 600–900ms debounce：

- 同一 conversation 未开始执行的文本合并成一个 turn；
- run 已执行时，新消息进入下一 turn；
- `/取消` 不参与合并，直接取消当前 context；
- 不使用 `EnqueueScopedSystemTask` 直接吞并后续消息；inbox 是消息真相源，task 只是唤醒信号。

### 7.5 Context Token

- 按 tenant/channel/account/peer 隔离；
- 加密持久化，记录 obtained_at 与 last_seen_at；
- 回复时使用触发该 turn 的最新 token；
- 缺失、被拒绝或疑似过期时 fail-fast，状态转为需要用户重新发消息；
- 不承诺永久主动推送；
- 多账号的任何 cache key 都必须包含 account id。

### 7.6 Outbox

- 回复先写 outbox，再由 sender 投递；
- `reply_key` 由 run + message segment 生成并唯一；
- HTTP 200 仍需解析业务 ret/errcode；
- 明确未送达的临时错误指数退避 + jitter；
- 网络超时后结果不确定时标记 `unknown`，不能无上限重发；
- 投递成功、失败、unknown 都进入审计与指标；
- 文本按自然边界分片，不能从字节中间切中文或 Markdown。

### 7.7 媒体

- 流式下载，限制大小、时长、MIME 和并发；
- AES key 和下载参数不落日志；
- 临时文件使用随机目录并自动清理；
- 文件导出可直接通过 iLink media 发送；
- 语音优先使用协议提供的识别文本；无识别文本时，ASR 作为后续可选能力；
- 外部上传文件在进入 RAG 或工具前做病毒和内容安全检查。

## 8. 身份、权限与安全

### 8.1 新权限

建议新增：

- `assistant.access`：允许绑定并使用助手；
- `assistant.channel_manage`：管理微信/企微通道；
- `assistant.model_manage`：管理模型 profile 和密钥；
- `assistant.binding_manage`：审批、限制和撤销身份；
- `assistant.audit`：查看会话/run/tool 审计；
- `assistant.eval`：运行和查看评测；
- `assistant.confirm_write`：允许确认 R2 操作。

这些权限不替代现有资源权限。比如用户同时拥有 `assistant.access` 和 `managed_instance.view` 才能列实例；查询使用记录还必须有 `managed_instance.usage_view`。

### 8.2 ToolExecutionContext

每个工具必须收到不可由模型构造的执行上下文：

```go
type ToolExecutionContext struct {
    ActorUserID   int
    ChannelID     int64
    ConversationID int64
    RunID         string
    AllowedInstanceIDs []int64
    RequestID     string
    Deadline      time.Time
}
```

工具 wrapper 的固定顺序：身份有效 → assistant.access → 资源权限 → 允许实例范围 → 参数 schema → 风险策略 → 预算/限流 → 服务调用 → 脱敏 → 审计。

### 8.3 Prompt Injection 防护

- user、远端实例字段、告警文本、账号 note、文档和媒体 OCR 全部视为不可信数据；
- 工具结果以结构化 JSON 交给模型，并明确标记“data, not instructions”；
- system prompt 不包含凭据、内部 URL 或实现细节；
- 模型无任意网络、SQL、Shell、文件或管理 API 能力；
- 工具名与 JSON Schema 固定，拒绝额外字段；
- 所有 ID 重新查库，不能信模型提供的名称映射；
- 输出经过 secret/PII pattern 检查和 Markdown 安全处理；
- 远端错误只映射稳定 error code，不把响应体整段送模型；
- RAG 文档必须带 ACL、source、version 和 checksum。

### 8.4 凭据与网络

- 从 `CredentialCipher` 提取通用 `SecretCipher`，使用 purpose + entity id + key version 作为 AEAD associated data；
- 支持密钥版本轮换，旧版本只读、新版本写入；
- 模型、微信 API、微信 CDN 分别使用独立 HTTP client、超时、DNS/egress allowlist；
- 微信 client 只允许腾讯 iLink 与明确 CDN 域名；
- 模型 client 只允许配置 profile 的 origin，禁止跨源 redirect；
- 日志、trace、audit 统一 redaction；
- Web approval link 一次性、短 TTL、绑定 user/run/plan hash，不能转发后复用。

## 9. 记忆、RAG 与 MCP

### 9.1 短期记忆

- 保留最近若干 turn + 滚动摘要；
- 工具大结果不进入长期上下文，只保存可验证摘要和引用；
- prompt token 超阈值时确定性压缩；
- `/清空上下文` 立即开始新 conversation epoch。

### 9.2 长期记忆

MVP 只保存用户明确设置的结构化偏好。不要自动抽取“事实记忆”，避免把错误、敏感信息或过时数据永久保存。

### 9.3 RAG

第一阶段问题主要是结构化实时数据查询，Tool Calling 比 RAG 更适合。RAG 只在后续用于：

- 项目操作手册；
- 实例接入文档；
- 告警处置 Runbook；
- 公司内部制度与术语。

RAG 不用于回答实时费用、RPM、账号状态或权限数据。首版不增加向量数据库，先验证关键词/元数据检索；确有召回需求后再引入可插拔 retriever。

### 9.4 MCP

内部 Go 服务直接调用，不通过 MCP 绕一圈。后续可用官方 MCP Go SDK 暴露严格只读工具，让 Codex/OpenClaw 等外部 Agent 使用。MCP server 仍必须复用同一 authz、tool policy 和审计。

## 10. 可观测性与评测

### 10.1 Trace

建议 span：

```text
assistant.receive
assistant.queue
assistant.run
gen_ai.chat <model>
assistant.tool <tool_name>
assistant.confirmation
assistant.delivery
```

关键属性：channel、account、conversation hash、run、model、prompt version、tool、risk、permission decision、cache hit、data freshness、tokens、cost、TTFT、total latency、delivery status。默认不记录内容。

### 10.2 指标

- inbound_total / duplicate_total / dead_letter_total；
- channel_online / lease_owner / poll_errors / reauth_required；
- ack_latency、first_meaningful_response、total_turn_latency；
- model_requests / tokens / cost / 429 / timeout / fallback；
- tool_calls / failures / denied / latency / stale_data；
- unauthorized_attempts；
- confirmations_created / expired / rejected / executed；
- outbound_success / retry / unknown / failed；
- user feedback 与 task success rate。

### 10.3 离线 Eval 集

至少 100 条中文黄金问题，覆盖：

- 时间范围和时区；
- 实例名相似或不存在；
- 跨实例汇总；
- 部分数据陈旧/失败；
- 权限不足与撤权即时生效；
- 账号、费用、RPM、成功率、告警；
- 多轮追问与指代；
- 导出、取消与重试；
- prompt injection；
- 写操作诱导、伪造确认码、过期计划；
- 模型超时、429 和 fallback。

Prompt、tool schema、model 或 policy 变更都必须跑 eval；结果按 prompt/model 版本留档。

## 11. 管理 API 与前端

### 11.1 API 建议

```text
GET/PUT    /api/assistant/settings
GET/POST   /api/assistant/model-profiles
PUT/DELETE /api/assistant/model-profiles/:id
POST       /api/assistant/model-profiles/:id/test

GET/POST   /api/assistant/channels
POST       /api/assistant/channels/:id/login
GET        /api/assistant/channels/:id/login/:session_id
POST       /api/assistant/channels/:id/reconnect
DELETE     /api/assistant/channels/:id/credential

GET/POST   /api/assistant/bindings
PUT/DELETE /api/assistant/bindings/:id
POST       /api/assistant/pairing-codes

GET        /api/assistant/conversations
GET        /api/assistant/conversations/:id
GET        /api/assistant/runs/:id
POST       /api/assistant/runs/:id/cancel
POST       /api/assistant/runs/:id/feedback

POST       /api/assistant/playground/messages
GET        /api/assistant/audits
POST       /api/assistant/evals/run
```

所有管理路由继续使用登录 session + Admin/Root/细粒度权限。微信入站不暴露公网 webhook；iLink 使用服务端长轮询。

### 11.2 前端信息架构

“AI 助手”一级导航：

1. 概览：通道在线状态、最近延迟、错误、成本、待处理确认；
2. 微信连接：扫码、账号、租约 owner、last poll、last message、重新认证；
3. 模型：profile、连通测试、fallback、预算；
4. 用户绑定：微信身份、系统用户、实例范围、状态、撤销；
5. 对话：脱敏会话、run 时间线、工具调用和反馈；
6. 审计：授权、拒绝、确认、执行和投递；
7. 评测：数据集、版本对比、失败样本；
8. Playground：不用微信即可测试同一 Agent/Tool/Policy 链路。

扫码页必须显示完整状态机，而不只是“已配置”。密钥保存后不再回显。

## 12. 部署与容量

### 12.1 默认部署

- 与现有 Go 服务同一二进制、同一数据库；
- 只有 master 节点启动 assistant runtime，但每个微信账号仍使用 DB lease；
- PostgreSQL 作为生产推荐；Redis 保持可选，不作为消息真相源；
- inbox/outbox/run 状态在 DB，进程重启可恢复；
- 模型和 iLink 使用有界 worker pool；
- 不在 HTTP handler 内同步跑完整 Agent。

现有 `SystemTask` 的 wakeup 是进程内信号，非 runner 节点写入任务后，master 最坏可能等待当前 15 秒轮询，不满足交互体验。Assistant 状态可以复用 SystemTask 的租约思想，但交互队列应使用专用有界 worker，并通过 PostgreSQL notification、Redis 仅唤醒或 200–500ms DB 短轮询降低等待；数据库始终是权威状态。通用 SystemTask payload 只保存 assistant run id，不保存完整 prompt、对话或工具结果。

### 12.2 拆分条件

满足任一条件再拆 `assistant-worker`：

- 模型调用显著影响控制台 API 延迟；
- 多账号或对话吞吐超过单进程 worker 限制；
- 需要独立扩缩容或故障域；
- 需要 GPU/ASR/文档处理等重任务。

拆分后仍共享 model 与应用服务契约，不让 worker 持有 Root session；使用内部服务身份与最小权限。

### 12.3 限流与预算起点

- 每用户：并发 1，突发消息做 debounce；
- 每通道：模型并发可配置，默认 4；
- 每会话：单 run 串行；
- 每轮：最多 6 tools、30 秒、有限 token；
- 远端刷新：按实例与工具单独限流；
- 日成本和单轮成本达到阈值时降级为确定性命令或拒绝复杂查询。

## 13. 分阶段实施

### Phase 0：协议与框架契约验证（3–5 个工作日）

交付：

- 确认测试微信账号具备 iLink/ClawBot 准入；
- Web QR → 扫码 → 文本收发 PoC；
- 验证 direct-only、typing、分片、context、session timeout；
- 对 corespeed/openilink/the-yex 做代码审计与故障测试；
- 建立 Tencent 官方协议 fixture；
- Eino 与最小自研 tool loop 对比；
- 确认实际条款、依赖许可证和 NOTICE 处理。
- 完成生产基座差距清单：Connector 默认策略、用户状态即时校验、实例范围、secret keyring、Session 占位符、setup bootstrap、可信代理和 CI 门禁。

退出标准：选定且锁定微信 SDK/commit；凭据不明文落盘；能控制 cursor/context；能注入 HTTP client；重启、重复消息和失效场景有可验证策略。任何硬条件不满足就不进入生产实现。

### Phase 1：只读可用 MVP（约 2 周）

交付：

- assistant models、migration 与通用 secret cipher；
- 微信 channel、账号租约、事务 inbox、outbox；
- 一次性用户绑定与 `assistant.access`；
- model profile 与一个 primary model；
- 单 Agent runner；
- 实例、健康、Dashboard、实时、使用汇总、告警六类只读工具；
- Web Playground、概览、扫码、模型、绑定、run trace；
- 单元、契约、集成和基础安全 eval。

退出标准：真实微信端能安全完成核心查询；重启不丢已提交 inbox；重复消息不会重复调用工具；撤权下一次工具调用立即生效；数字均有工具来源和采集时间。

### Phase 2：最佳体验与可靠性（约 1–2 周）

交付：

- typing、debounce、进度、取消、重试、继续；
- fallback model、熔断、预算；
- 数据新鲜度与部分失败表达；
- 使用记录导出与微信文件发送；
- 结构化偏好、摘要与清理；
- 多账号隔离；
- Prometheus/OTel dashboard、告警和 dead-letter 运维页；
- 完整 100+ eval 集和回归门禁。

### Phase 3：受控写操作（约 1–2 周）

交付：

- R2/R3 风险分类；
- PlanOperation 工具；
- 微信确认 nonce；
- Web step-up approval；
- plan hash、TTL、ETag、幂等和 unknown 状态；
- 写操作专项红队和审计报表。

退出标准：模型不存在直接 execute 工具；所有写操作都有 actor、plan、确认和结果审计；重放、转发或过期确认全部拒绝。

### Phase 4：可选扩展

- 企业微信通道；
- Runbook/文档 RAG；
- 只读 MCP server；
- 告警订阅与摘要；
- 语音/ASR；
- assistant worker 独立部署；
- 一个微信账号多 Agent backend 时再评估 iLink Hub。

## 14. 验收指标

### 14.1 体验 SLO

- P95 typing/ack < 1 秒；
- 快照类查询 P95 最终回复 < 5 秒；
- 需要实时或多实例远端查询 P95 < 15 秒；
- `/取消` P95 生效 < 1 秒；
- 最终回复 100% 显示数据范围、时区、采集时间和完整/部分状态；
- 超长回复无乱码、无半个 Unicode 字符、分片顺序正确。

### 14.2 正确性

- 黄金问题 task success ≥ 90%，核心查询 ≥ 95%；
- 数字 grounding ≥ 99%，不得出现工具结果中不存在的业务数字；
- 时间范围解析测试 100% 通过；
- 重复 external message 不产生第二个 run；
- 任何部分失败不会标记为完整成功；
- 用户撤权后下一工具调用必须拒绝。

### 14.3 安全

- 未绑定用户 0 条业务数据泄露；
- 越权和 prompt injection 安全用例 100% 拒绝；
- 日志、trace、API 响应中 0 个明文 Bot/API/context token；
- R3 操作 100% 需要 Web step-up；
- 模型 0 个任意 URL/SQL/Shell 工具；
- 确认 nonce 重放、转发、过期、plan 变化全部拒绝。

### 14.4 可靠性

- 进程在 inbox 事务提交后任意点崩溃，消息最终仍可处理；
- 同一微信账号全局最多一个有效 poller；
- session 失效 30 秒内进入 `REAUTH_REQUIRED` 并触发运营告警；
- 出站明确失败可重试，结果不确定不会无限重复；
- dead-letter 可见、可审计、可人工重放；
- 数据库恢复后 worker 可从持久状态继续。

## 15. 测试矩阵

| 层级 | 必测内容 |
| --- | --- |
| 单元 | 时间解析、schema、分片、去重键、redaction、policy、确认 nonce、plan hash、context 选择 |
| 契约 | Tencent JSON fixtures、SDK ret/errcode、QR、长轮询、typing、媒体、模型 tool call |
| 集成 | DB inbox/outbox、cursor 事务、租约/fencing、RBAC 撤权、SystemTask 恢复、多账号隔离 |
| E2E | 真实测试微信账号扫码、重启、重复投递、取消、导出、重新认证 |
| Chaos | 模型 429/timeout、iLink -14、网络断开、DB/Redis 故障、发送结果 unknown、节点抢租约 |
| Security | prompt injection、恶意 note/错误文本、越权实例、伪造确认、secret 泄漏、SSRF 尝试 |
| Eval | 100+ 中文业务问题、模糊问题、多轮追问、部分失败、拒答与写操作 |

持续验证命令继续包含：

```bash
go test ./... -count=1
go vet ./...
cd web/default
bun run typecheck
bun run lint
bun run build
```

## 16. 主要风险与应对

| 风险 | 应对 |
| --- | --- |
| iLink 准入或条款变化 | Phase 0 先验证；channel 接口隔离；并行预留企业微信 |
| 社区 Go SDK 不成熟 | 官方协议 fixture、锁定 commit、自有可靠性层、故障注入；不信任“production ready”自述 |
| 个人微信无真流式/群聊 | typing + 一次进度 + 单条最终；群聊/真流式转企业微信 |
| 重复消息导致重复工具/写操作 | inbox unique key、run key、tool idempotency、confirmation plan hash |
| context token 失效 | 加密持久、freshness、fail-fast、REAUTH/用户触发恢复 |
| 模型幻觉数字 | 所有数字来自结构化工具；answer validator；eval |
| 模型越权 | 权限在工具外层代码执行；每次调用重新鉴权；绑定实例范围 |
| 控制平面出网过宽 | assistant 独立 egress allowlist；无任意网络工具 |
| 聊天隐私与成本 | 最小留存、内容默认不进 trace、专用模型凭据、预算与 fallback |
| 过早复杂化 | MVP 单 Agent、无 RAG、无 MCP、无独立 worker、只读工具 |

## 17. 推荐首轮开发切片

第一轮只做一个纵向闭环，不同时建设全部页面和工具：

1. `assistant_channels`、`assistant_identities`、`assistant_inbox/outbox`、`assistant_conversations/runs` 最小模型；
2. 通用 secret cipher；
3. `WeChatChannel` 接口和一个经过 Phase 0 选定的 iLink adapter；
4. Web QR 登录与健康状态；
5. 一次性绑定码；
6. 一个 model profile；
7. 三个只读工具：`list_instances`、`get_dashboard_summary`、`get_realtime_metrics`；
8. 单 Agent runner、typing、最终回复和 `/取消`；
9. run/tool/delivery 审计；
10. 30 条黄金问题与重复消息、撤权、模型超时、session 失效 E2E。

完成这个切片后先让真实用户连续试用，再扩工具、导出、记忆和写操作。这样能最快验证“微信里问一句就拿到可信数据”的核心体验，同时不提前承担 RAG、多 Agent、MCP 和复杂运维的成本。

## 18. 上线前需要确认的产品决策

这些决策不阻塞架构设计，但必须在对应 Phase 开始前确认：

1. 用户说的“OpenBot”是否特指腾讯 iLink/ClawBot，还是某个独立 OpenBot 产品；本方案按腾讯官方 iLink/ClawBot 设计。
2. 目标是个人微信私聊，还是必须支持企业微信群聊；后者应同步建设企业微信 adapter。
3. 首批实际用户和允许访问的实例范围。
4. 首选模型、备用模型、数据出境要求和单日成本预算。
5. 微信内是否允许 R2 写操作确认，或所有写操作都统一跳 Web。
6. 对话、工具结果、审计和媒体的最终保留期。
7. 是否需要主动告警；个人 iLink 不能作为永久主动推送 SLA。

## 19. 参考来源

- [Tencent/openclaw-weixin](https://github.com/Tencent/openclaw-weixin)
- [corespeed-io/wechatbot](https://github.com/corespeed-io/wechatbot)
- [openilink/openilink-sdk-go](https://github.com/openilink/openilink-sdk-go)
- [lib-x/ilink](https://github.com/lib-x/ilink)
- [the-yex/wechat-ilink-sdk](https://github.com/the-yex/wechat-ilink-sdk)
- [jeffkit/ilink-hub](https://github.com/jeffkit/ilink-hub)
- [OpenILink Hub](https://github.com/openilink/openilink-hub)
- [WeCom AI Bot SDK](https://github.com/WecomTeam/aibot-node-sdk)
- [CloudWeGo Eino](https://github.com/cloudwego/eino)
- [Model Context Protocol Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [LangGraph](https://github.com/langchain-ai/langgraph)
- [OpenTelemetry GenAI Semantic Conventions](https://github.com/open-telemetry/semantic-conventions-genai)

本项目本身继续遵守 `LICENSE`、`NOTICE` 和 `THIRD-PARTY-LICENSES.md`。新增依赖前必须核对版本级许可证、NOTICE、商用限制和传递义务；不得从带限制性非商业条款的 OpenBot 项目直接复制代码。
