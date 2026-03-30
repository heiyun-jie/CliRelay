# 30-02 `/v1` 运行时热更新治理

## 1. 概述

`/v1` 请求链已经接入本地记忆、SCE 检索和 Intent Upgrade，但其中一部分运行时资源与配置仍存在“启动时捕获、运行中不更新”的问题。

本 Spec 的目标是：

1. 明确 `/v1` 链路中的运行时可热更新边界
2. 让 SCE engine 与 Intent Upgrade 配置在运行中更新后对后续请求生效
3. 同步 `.sce` 与仓库文档，避免治理结论和代码现实脱节

## 2. 需求

### 2.1 明确运行时资源热切换边界

**验收标准**

1. `/v1` 中间件不应继续长期持有启动时的旧 `sce.Engine`
2. 后续请求应读取当前活动的 SCE engine
3. SCE engine 被替换或关闭时，旧连接有明确释放路径

### 2.2 Intent Upgrade 配置热更新生效

**验收标准**

1. `intent-upgrade.enable` 更新后，无需重启即可对后续请求生效
2. `intent-upgrade.model`、`api-keys`、`exclude-models`、`timeout-ms`、`max-input-tokens` 更新后，对后续请求生效
3. 更新后不应继续沿用旧配置快照

### 2.3 文档与 Spec 同步

**验收标准**

1. `.sce/specs/README.md` 中存在当前 active spec 入口
2. `.sce/steering/CURRENT_CONTEXT.md` 反映新的活跃 spec 与当前优先问题
3. `SCE_GATEWAY_TASKS.md` 或 `docs/project-status_CN.md` 至少一处更新为当前真实状态
