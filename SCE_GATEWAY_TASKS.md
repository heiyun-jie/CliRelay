# CliRelay + SCE 接管文档

> 这份文档面向后续接手该仓库的人。
> 目标不是罗列所有历史，而是快速回答三个问题：
> 1. 这个项目现在是什么结构
> 2. 记忆系统和 SCE 已经做到哪里
> 3. 接下来优先该处理什么

## 1. 项目当前状态

当前仓库已经不是单纯的上游代理壳子，而是一个完整的 AI 网关项目，包含四层能力：

1. 代理层：统一接入 Claude / Gemini / Codex / OpenAI-compatible / Vertex / Amp 等上游。
2. 管理层：`/v0/management` 提供配置、监控、日志、鉴权文件、记忆等接口。
3. 面板层：`management-panel/` 是前端控制台源码，服务端可直接托管 `/manage`。
4. 记忆层：本地 `usage.db` 记忆 + SCE `runtime.db/index.json` 项目知识注入。
5. 审计层：模型请求日志与管理访问审计已分层持久化。

当前已经落地的记忆相关能力：

1. `usage.db` 已支持 `memory_entries`、`memory_application_logs`、`conversation_turns`。
2. `/v1` 请求在进入模型前会自动注入本地记忆、近期对话、SCE 项目知识。
3. 请求完成后会自动写回 `conversation_turns`，且支持存储 `assistant_text`。
4. 管理接口已支持查看记忆条目、命中记录、最近轮次。
5. 管理面板已有“记忆中心”页面。
6. SCE 记忆已支持导入到 `memory_entries`，并已压缩为“锚点式记忆卡片”。

## 2. 核心目录

### 后端主线

- `cmd/server/main.go`
  入口，支持正常服务模式和若干导入/登录模式。
- `internal/api/server.go`
  Gin 路由注册、中间件装配、管理接口注册、面板托管。
- `internal/config/config.go`
  主配置结构，已经包含 `sce-memory` 和 `intent-upgrade` 配置段。

### 本地记忆

- `internal/usage/usage_db.go`
  SQLite 初始化。
- `internal/usage/memory_db.go`
  本地记忆、命中记录、对话轮次的表结构与读写逻辑。
- `internal/api/handlers/management/memory.go`
  记忆相关管理接口。

### 请求注入

- `internal/api/memory_middleware.go`
  负责读取本地记忆、近期对话、SCE 结果并注入到请求体。
- `internal/api/intent_upgrade.go`
  可选的二次意图升级中间件，当前默认应视为“实验特性”。

### SCE 集成

- `internal/sce/engine.go`
  内嵌 SCE 查询引擎，直接读取 `runtime.db` 与 `index.json`。
- `internal/sce/types.go`
  SCE 命中结果与注入结构体。
- `internal/cmd/sce_memory_import.go`
  将 SCE memory 目录导入到 CliRelay 本地记忆库。

### 前端控制台

- `management-panel/src/modules/memory/MemoryPage.tsx`
  记忆中心页面。
- `management-panel/src/lib/http/apis/memory.ts`
  记忆中心调用的接口封装。
- `management-panel/src/modules/monitor/RequestLogsPage.tsx`
  模型请求日志页面，现已展示来源字段。
- `management-panel/src/modules/monitor/AccessAuditPage.tsx`
  管理访问审计页面，独立展示 `/v0/management/*` 访问记录。

### 请求审计

- `internal/usage/usage_db.go`
  `request_logs` 已保存 `client_ip`、`forwarded_for`、`user_agent`、`request_path`。
- `internal/usage/access_audit_db.go`
  管理访问审计表与查询逻辑。
- `internal/api/handlers/management/handler.go`
  管理鉴权中间件会对允许/拒绝访问都写入审计日志。

## 3. 运行时数据流

当前 `/v1` 请求的大致路径如下：

```text
客户端
  -> CliRelay /v1/*
  -> Auth / Quota / ModelRestriction / SystemPrompt
  -> MemoryHydrationMiddleware
       -> usage.db / memory_entries
       -> usage.db / conversation_turns
       -> SCE runtime.db + index.json
  -> IntentUpgradeMiddleware（可选）
  -> Handler / Translator / Executor
  -> 上游模型
  -> 回写 conversation_turns
  -> 返回客户端
```

这里最关键的现实结论：

1. Codex 不是直接去命中模型，而是先进入 CliRelay。
2. 记忆命中发生在 CliRelay，不发生在 Codex 客户端内部。
3. SCE 当前是“项目知识检索器”，本地 `usage.db` 当前是“会话记忆 + 手工记忆仓库”。

## 4. 记忆体系当前怎么分工

### A. 本地 `usage.db`

适合存：

1. 用户和模型的历史对话。
2. 手工创建的长期偏好、约束、项目锚点记忆。
3. 从 SCE 导入并压缩过的项目经验卡片。

当前特点：

1. 读取快，和 CliRelay 请求链路耦合紧。
2. 管理面板可直接查看。
3. 目前匹配逻辑主要还是文本/token 命中，不是完整的图谱联想。

### B. SCE `runtime.db` + `index.json`

适合存：

1. 项目规则、经验、代码符号、文件索引。
2. 更偏“项目知识”和“代码上下文”的内容。

当前特点：

1. 可按 query 做项目知识召回。
2. 命中结果包含规则、pattern、symbol、file 等多类条目。
3. 现在已经接进 `MemoryHydrationMiddleware`，但仍然是检索增强，不是统一主记忆库。

### C. 当前实际效果

当前系统已经能做到：

1. 下一次聊天时自动带入相关对话历史。
2. 在问题涉及项目规则或代码语义时，补充 SCE 知识命中。
3. 在本地记忆中保存压缩后的项目锚点卡片。

还做不到：

1. 像真正图数据库一样按实体关系扩散。
2. 自动判断“哪些对话值得沉淀为长期记忆”并高质量升级。
3. 把 SCE 和本地 memory 做成一个统一的管理、评分、淘汰体系。

## 5. 当前确认存在的问题

以下问题是已经结合代码重新核对过的，不是历史猜测。

### P0. `/v1` 请求体重复读取问题已收口

当前状态：

1. `/v1` 已增加统一 `BodyCacheMiddleware`。
2. `ModelRestrictionMiddleware`、`SystemPromptMiddleware`、`MemoryHydrationMiddleware`、`IntentUpgradeMiddleware` 已统一优先读取缓存 body。
3. 中间件改写 body 后会回写缓存，供下游继续读取。

当前结论：

1. 这一项已从“待修问题”变为“已完成稳定性收口”。

### P0. SCE Engine 生命周期已建立关闭路径

当前状态：

1. `Server.Stop()` 已在 HTTP server 关闭后显式释放当前 `sce.Engine`。
2. `/v1` 链路已改成请求时动态读取当前 engine，不再固定捕获启动时实例。
3. `SCEMemory` 配置更新后，会替换运行中的 engine，并释放旧连接。

当前结论：

1. 这一项已从“待修问题”变为“已完成生命周期收口”。

### P1. SCE 查询优化已完成第一阶段

当前状态：

1. `userMemoryHits()`、`symbolHits()`、`codeHits()` 已从“全量读取整表”收口为“SQL 粗筛候选 + Go 侧精排”。
2. 单次 hydration 现在会限制候选规模，不再把整表全部搬进 Go。
3. `buildAppliedUserMemory()` 已修正为优先查匹配项，不再只看最近 N 条。

当前结论：

1. 第一阶段优化已经落地，但还没有进入 FTS / 专门索引阶段。
2. 后续如果 SCE 数据继续明显膨胀，仍需要更深层索引优化。

### P1. Intent Upgrade 仍属于实验能力

现状：

1. 它会额外走一次本地 HTTP 回调分析意图。
2. 还会异步把分析结果写回 SCE `user_memory`。
3. 当前已支持对后续请求热更新 `intent-upgrade` 配置，不再固定使用启动快照。

风险：

1. 增加链路复杂度。
2. 回写内容质量不一定稳定。
3. 如果没有严格开关和范围控制，容易把低质量“自动结论”写进长期记忆。

### P1. 本地记忆与 SCE 记忆仍是“两套系统”

现状：

1. 本地 `memory_entries` 已经能承载长期记忆。
2. SCE 也能返回自己的 user memory / rule / code hits。

问题：

1. 两边没有统一评分模型。
2. 没有统一淘汰、升级、人工审查流程。
3. 管理面板只直接展示本地 memory，不直接展示 SCE 内部用户记忆。

### P1. 请求来源审计已完成第一版收口

现状：

1. 模型请求日志已经能区分来源机器。
2. 管理后台访问审计已独立落库，不再复用模型请求日志。
3. 控制台已分开展示“请求日志”和“访问审计”。

剩余空间：

1. 访问审计还没有导出能力。
2. 更细粒度筛选项还可以继续补。

## 6. 当前建议的推进顺序

如果继续做，不建议同时铺太多线。建议按下面顺序推进：

1. 下一步评估是否需要 FTS / 专门索引，继续降低 SCE 查询成本。
2. 再决定 `IntentUpgradeMiddleware` 是继续保留、弱化，还是下线。
3. 最后再考虑“统一记忆模型”，把 SCE user memory、本地 memory_entries、conversation_turns 做成明确分层。

## 7. 当前已完成但需要记住的事实

这些是本项目当前最容易被误解的点：

1. 管理接口 `GET /v0/management/memory` 已经存在，不再是待开发。
2. 前端“记忆中心”页面已经存在，不是空白设计稿。
3. `conversation_turns` 现在已支持 `assistant_text`。
4. SCE 导入到本地记忆时，已经不是整篇 markdown，而是“Anchor/Core/Recall/Branches/Source”压缩卡片。
5. Codex 当前通过 CliRelay 调模型，因此记忆命中点在 CliRelay，不在 Codex 客户端。

## 8. 交接时的最小核对清单

接手人建议先做这几步确认环境：

1. 启动 CliRelay，确认 `/v1/models` 和 `/v0/management/models` 可访问。
2. 确认 `data/usage.db` 中存在 `memory_entries`、`memory_application_logs`、`conversation_turns`。
3. 确认 `sce-memory` 配置指向真实的 `runtime.db` 与 `index.json`。
4. 打开 `/manage/#/memory`，确认能读到记忆条目。
5. 发一次真实请求，确认 `conversation_turns` 和 `memory_application_logs` 有新增数据。

## 9. 文档入口

更正式的当前项目总览见：

- `docs/project-status_CN.md`
