# 00-00 项目参考

## 1. 项目一句话

CliRelay 是一个多上游 AI 网关，附带管理 API、Web 控制台、请求日志与记忆增强能力。

## 2. 关键代码入口

- 服务入口：`cmd/server/main.go`
- 路由装配：`internal/api/server.go`
- 本地记忆：`internal/usage/memory_db.go`
- 记忆注入：`internal/api/memory_middleware.go`
- SCE 引擎：`internal/sce/engine.go`
- 管理接口：`internal/api/handlers/management/`
- 前端控制台：`management-panel/`

## 3. 关键文档入口

- 项目接管文档：`SCE_GATEWAY_TASKS.md`
- 项目总览：`docs/project-status_CN.md`
- SCE 接管总览：`.sce/docs/guides/PROJECT_TAKEOVER_GUIDE.md`

