# 30-03 SCE 查询优化

## 1. 概述

当前 `internal/sce/engine.go` 的 `userMemoryHits()`、`symbolHits()`、`codeHits()` 仍然主要依赖 “全量读取 -> Go 侧评分 -> 截断” 的方式。

本 Spec 的目标是：

1. 把明显的全量候选读取收口为“数据库粗筛 + Go 精排”
2. 保持当前命中语义基本稳定，不为了优化直接重写召回模型
3. 为下一阶段更深层的索引优化预留清晰入口

## 2. 需求

### 2.1 候选读取收口

**验收标准**

1. `user_memory` 查询不再无条件读取全部 active 行
2. `symbol_index` 查询不再无条件读取全部行
3. `file_index` 查询不再无条件读取全部行
4. 至少有候选上限，避免单次 hydration 把整表全部搬进 Go

### 2.2 保持当前召回语义

**验收标准**

1. 仍保留 Go 侧评分与排序逻辑
2. 热门命中类型 `user-memory`、`code:symbol`、`code:file` 仍可被召回
3. `buildAppliedUserMemory()` 不能因为只看最近 N 条而漏掉更旧但匹配的记录

### 2.3 文档同步

**验收标准**

1. `.sce/specs/README.md` 中存在本 Spec
2. `.sce/steering/CURRENT_CONTEXT.md` 反映当前优化阶段
3. `SCE_GATEWAY_TASKS.md` 或 `docs/project-status_CN.md` 至少一处说明本轮优化已完成第一阶段
