# 环境说明

## 1. 仓库

- 仓库根目录：`D:\XM\QD\CliRelay`
- 主分支：`main`

## 2. 当前关键运行文件

- 本地配置：`config.local-dev.yaml`
- 运行数据库：`data/usage.db`

## 3. 关键外部依赖

### 本地记忆数据库

- `CliRelay/data/usage.db`

### SCE 项目知识来源

- SCE memory root：`D:\XM\QD\yangzhou\.sce\memory`
- SCE runtime db：由 `config.local-dev.yaml` 中 `sce-memory.db-path` 指定

## 4. 当前已确认的记忆相关事实

1. `memory_entries` 已有导入数据。
2. `conversation_turns` 已支持 `assistant_text`。
3. 管理接口 `/v0/management/memory*` 已存在。
4. 管理面板已有记忆中心页面。

