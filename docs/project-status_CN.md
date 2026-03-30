# CliRelay 当前项目总览（中文）

## 1. 文档目的

本文档用于描述当前仓库的实际结构、核心模块、记忆系统现状和后续建议。
重点是“当前代码已经是什么样”，而不是复述历史演化。

相关交接清单见：

- `../SCE_GATEWAY_TASKS.md`

## 2. 项目定位

CliRelay 现在可以视为一个“多上游 AI 网关 + 管理控制台 + 记忆增强层”的组合项目，而不是单一代理。

主要职责包括：

1. 统一代理多个 AI 上游。
2. 提供管理 API 与 Web 控制台。
3. 持久化请求日志、对话轮次、长期记忆。
4. 在请求进入模型前自动补充本地记忆与 SCE 项目知识。

## 3. 代码结构总览

### 服务入口

- `cmd/server/main.go`
  主入口，负责参数解析、配置加载、切换不同运行模式。

### 服务装配

- `internal/api/server.go`
  注册 `/v1`、`/v0/management`、`/manage` 等核心路由。

### 配置定义

- `internal/config/config.go`
  包含通用代理配置，以及：
  - `sce-memory`
  - `intent-upgrade`

### 数据持久化

- `internal/usage/usage_db.go`
  SQLite 初始化与主表创建。
- `internal/usage/memory_db.go`
  本地记忆、命中日志、对话轮次。

### 记忆增强

- `internal/api/memory_middleware.go`
  本地记忆 + 最近对话 + SCE 查询结果注入。
- `internal/api/intent_upgrade.go`
  可选的意图升级中间件。
- `internal/sce/engine.go`
  内嵌 SCE 检索引擎。
- `internal/cmd/sce_memory_import.go`
  SCE -> 本地记忆导入器。

### 管理控制台

- `internal/api/handlers/management/*`
  后端管理 API。
- `management-panel/`
  控制台前端源码。

## 4. 核心运行路径

### 4.1 请求路径

用户请求进入 `/v1/*` 后，大致经过：

1. 鉴权
2. 限流
3. 模型限制
4. 系统提示词处理
5. `MemoryHydrationMiddleware`
6. `IntentUpgradeMiddleware`（可选）
7. Handler / Translator / Executor
8. 上游 AI Provider

### 4.2 记忆注入路径

`MemoryHydrationMiddleware` 当前会组合三类上下文：

1. 本地 `memory_entries`
2. 本地 `conversation_turns`
3. SCE `HydrateContext(query, top)`

注入格式已经做过整理，不是简单拼原文，而是分段注入：

1. `Relevant memory`
2. `Project knowledge`
3. `Recent conversation`

## 5. 记忆系统当前实现

## 5.1 本地 SQLite 记忆

本地库 `data/usage.db` 当前关键表：

### `memory_entries`

用途：

1. 长期记忆
2. 人工添加记忆
3. 从 SCE 导入的压缩记忆卡片

核心字段：

1. `scope_type`
2. `scope_value`
3. `kind`
4. `content`
5. `tags_json`
6. `priority`
7. `always_apply`

### `memory_application_logs`

用途：

1. 记录每次注入命中了哪条记忆
2. 记录请求路径、query、match reason

### `conversation_turns`

用途：

1. 记录最近对话
2. 支持 `user_text`
3. 支持 `assistant_text`

这意味着当前系统已经支持“用户说了什么 + 模型回了什么”的成对保存。

## 5.2 SCE 项目知识检索

SCE 当前通过 `internal/sce/engine.go` 直接读取：

1. `runtime.db`
2. `index.json`

会产出两类结果：

1. `AppliedUserMemory`
2. `RelevantHits`

命中类型包含：

1. `memory:rule`
2. `memory:consolidated`
3. `code:symbol`
4. `code:file`

也就是说，SCE 当前不只是“规则卡片库”，还是一个轻量项目知识检索器。

## 5.3 SCE 导入到本地 memory_entries

仓库中已经有导入命令：

```bash
go run ./cmd/server -config config.local-dev.yaml -sce-memory-import <sce-memory-root>
```

导入时当前采用“锚点卡片”格式，而不是整篇 markdown：

```text
Anchor: ...
Core: ...
Recall: ...
Branches:
- ...
- ...
Source: ...
```

这样做的意义：

1. 更适合当前本地记忆匹配逻辑
2. 更适合作为会话时的快速补充
3. 比整篇文档更接近“锚点触发联想”的用法

## 6. 管理接口与面板现状

### 6.1 后端接口

当前已经存在的记忆相关接口：

1. `GET /v0/management/memory`
2. `POST /v0/management/memory`
3. `GET /v0/management/memory/applications`
4. `GET /v0/management/memory/turns?api_key=...`

### 6.2 前端页面

当前前端已具备：

1. 记忆中心页面
2. 最近轮次展示
3. 命中记录展示
4. 手工创建记忆条目

相关前端文件：

1. `management-panel/src/modules/memory/MemoryPage.tsx`
2. `management-panel/src/lib/http/apis/memory.ts`

## 7. 当前确认的问题

### 7.1 请求体重复读取

当前 `memory_middleware.go` 与 `intent_upgrade.go` 都会自己读请求体。

这属于当前最优先的工程问题，因为它直接影响：

1. 请求稳定性
2. 中间件叠加后的可维护性
3. 后续继续扩展注入链路的安全性

### 7.2 SCE Engine 的生命周期管理不完整

`Engine.Close()` 存在，但当前没有看到明确统一 shutdown 释放。

### 7.3 SCE 查询策略仍偏粗放

当前仍然主要靠 Go 侧打分，意味着数据量继续变大后会压性能。

### 7.4 双记忆系统尚未统一

当前是：

1. 本地 `usage.db` 负责会话记忆和长期记忆
2. SCE 负责项目知识检索

这在工程上可用，但在治理上仍是两套系统。

## 8. 推荐的后续治理方向

建议后续把工作分成三层，不要混做：

### 第一层：稳定性

1. Body cache
2. shutdown 释放
3. SCE 查询优化

### 第二层：记忆质量

1. 自动沉淀什么内容
2. 记忆升级规则
3. 低质量自动回写的抑制

### 第三层：统一治理

1. 本地记忆和 SCE user memory 的边界
2. 管理面板如何统一展示
3. 统一评分 / 淘汰 / 审查策略

## 9. 当前建议阅读顺序

第一次接手该项目，建议按下面顺序阅读：

1. `README.md`
2. `SCE_GATEWAY_TASKS.md`
3. `internal/api/server.go`
4. `internal/api/memory_middleware.go`
5. `internal/sce/engine.go`
6. `internal/usage/memory_db.go`
7. `internal/api/handlers/management/memory.go`
8. `management-panel/src/modules/memory/MemoryPage.tsx`

## 10. 结论

当前项目已经不是“是否有记忆系统”的阶段，而是“记忆系统已经接上了，但还需要工程化收口”的阶段。

一句话总结：

1. 本地记忆已可用
2. SCE 项目知识已接入
3. 面板已可查看
4. 导入链路已可用
5. 当前主要问题不在于“有没有”，而在于“如何稳定、统一、提质”

