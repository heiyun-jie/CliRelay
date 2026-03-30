# Spec 工作流

## 1. 什么时候新建 Spec

满足任一条件就应该建 Spec：

1. 影响请求链路
2. 影响数据库结构
3. 影响管理接口/控制台
4. 影响 SCE / 记忆系统治理

## 2. 最小 Spec 结构

```text
spec-name/
├── requirements.md
└── tasks.md
```

## 3. 执行规则

1. 先读 requirements
2. 再读 tasks
3. 执行中持续更新任务状态
4. 完成后同步更新相关文档

