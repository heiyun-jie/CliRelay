# Active Specs

## Active

### `00-00-project-reference`

用途：

1. 说明项目整体结构
2. 说明核心模块入口
3. 说明文档入口

入口：

- `.sce/specs/00-00-project-reference/README.md`

### `30-01-clirelay-sce-takeover`

用途：

1. 让仓库正式进入 SCE 管理方式
2. 收口当前接管问题
3. 给下一阶段改造提供统一任务入口

入口：

- `.sce/specs/30-01-clirelay-sce-takeover/requirements.md`
- `.sce/specs/30-01-clirelay-sce-takeover/tasks.md`

### `30-02-v1-runtime-hot-reload`

用途：

1. 规范 `/v1` 链路的运行时资源切换边界
2. 让 SCE engine 与 Intent Upgrade 配置支持热更新
3. 收口代码与文档的当前真实状态

入口：

- `.sce/specs/30-02-v1-runtime-hot-reload/requirements.md`
- `.sce/specs/30-02-v1-runtime-hot-reload/tasks.md`

### `30-03-sce-query-optimization`

用途：

1. 收口 SCE 的全量候选读取问题
2. 让查询进入 “SQL 粗筛 + Go 精排” 的第一阶段
3. 为后续更深层索引优化保留稳定入口

入口：

- `.sce/specs/30-03-sce-query-optimization/requirements.md`
- `.sce/specs/30-03-sce-query-optimization/tasks.md`
