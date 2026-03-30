# CliRelay SCE 接管指南

## 1. 接管时先看什么

1. `.sce/README.md`
2. `.sce/steering/CURRENT_CONTEXT.md`
3. `SCE_GATEWAY_TASKS.md`
4. `docs/project-status_CN.md`

## 2. 接管时先确认什么

1. 这个仓库已经有本地记忆系统
2. 这个仓库已经有 SCE 集成
3. 这个仓库已经有记忆中心前端
4. 当前首要任务是工程稳定性，而不是继续无边界扩展能力

## 3. 当前最重要的问题

1. 请求体重复读取
2. `sce.Engine` 关闭路径不明确
3. SCE 查询仍偏粗放
4. 本地记忆与 SCE user memory 治理边界未统一

## 4. 当前建议执行顺序

1. `30-01-clirelay-sce-takeover`
2. 后续新建一个 runtime 稳定性 Spec，先收口中间件链路

