# CliRelay — SCE 项目入口

> 根级 `.sce/` 是本仓库的 SCE 工作入口。
> 接手、排查、推进改造时，先看这里，再按索引进入，不要直接全盘扫描代码。

---

## 1. 项目定位

CliRelay 当前是一个“多上游 AI 网关 + 管理控制台 + 记忆增强层”组合项目。

当前重点不再是“是否能代理请求”，而是：

1. 稳定管理 `/v1` 请求链路。
2. 稳定管理 `/v0/management` 配置与控制台。
3. 让本地记忆与 SCE 项目知识在请求注入阶段可持续工作。

---

## 2. 最短阅读路径

1. 读本文件
2. 读 `.sce/steering/CURRENT_CONTEXT.md`
3. 读 `.sce/docs/guides/PROJECT_TAKEOVER_GUIDE.md`
4. 读 `.sce/specs/README.md`
5. 再按命中的 Active Spec 进入

---

## 3. 目录说明

```text
.sce/
├── README.md
├── version.json
├── steering/
│   ├── manifest.yaml
│   ├── CORE_PRINCIPLES.md
│   ├── ENVIRONMENT.md
│   ├── CURRENT_CONTEXT.md
│   └── RULES_GUIDE.md
├── specs/
│   ├── README.md
│   ├── WORKFLOW_GUIDE.md
│   ├── 00-00-project-reference/
│   └── 30-01-clirelay-sce-takeover/
├── docs/
│   ├── README.md
│   └── guides/
├── memory/
│   └── README.md
└── runtime/
    └── README.md
```

---

## 4. 当前默认入口

- 当前状态与问题：`.sce/steering/CURRENT_CONTEXT.md`
- 项目全景与接管说明：`.sce/docs/guides/PROJECT_TAKEOVER_GUIDE.md`
- SCE 规则索引：`.sce/steering/RULES_GUIDE.md`
- 当前 Active Spec：`.sce/specs/README.md`

---

## 5. 说明

当前仓库是第一次正式补齐 `.sce/` 结构。

因此这套目录的职责是：

1. 给后续模型/接手人一个统一入口。
2. 把仓库当前真实状态沉淀成可维护的 SCE 文档。
3. 把后续优先任务转成可执行 Spec，而不是只留在口头描述里。

