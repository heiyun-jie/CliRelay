# 当前上下文

## 1. 当前目标

当前目标不是继续解释项目，而是持续按 SCE 工作方式推进已有 Active Spec，并把代码、配置、文档状态保持一致。

这意味着：

1. 建立 `.sce/` 入口
2. 建立长期 steering
3. 建立 Active Spec
4. 让后续任务按 Spec 驱动推进

## 2. 当前已确认的项目现实

1. 仓库已经有本地记忆系统：
   - `memory_entries`
   - `memory_application_logs`
   - `conversation_turns`
2. 仓库已经有 SCE 集成：
   - `internal/sce/engine.go`
   - `internal/api/memory_middleware.go`
   - `internal/api/intent_upgrade.go`
3. 仓库已经有 SCE 导入命令：
   - `internal/cmd/sce_memory_import.go`
4. 仓库已经有前端记忆中心：
   - `management-panel/src/modules/memory/MemoryPage.tsx`
5. 仓库已经补齐请求来源审计第一版：
   - `request_logs` 已支持来源字段
   - `management_access_logs` 已独立落库
   - 控制台已拆分“请求日志”和“访问审计”

## 3. 当前活跃 Spec

- `00-00-project-reference`
- `30-01-clirelay-sce-takeover`
- `30-02-v1-runtime-hot-reload`
- `30-03-sce-query-optimization`
- `30-04-request-origin-audit`

## 4. 当前优先问题

1. 本地记忆和 SCE user memory 还没有统一治理边界。
2. 请求来源审计已经落地，但还没有继续扩展到更细粒度的筛选/导出能力。
3. 记忆质量治理仍缺少“自动沉淀/人工审核/淘汰”统一闭环。

## 5. 当前默认执行顺序

1. 任何新任务先查 `.sce/specs/README.md`
2. 如果命中现有 Active Spec，按 Spec 做
3. 如果不命中，先补 Spec，再做代码
