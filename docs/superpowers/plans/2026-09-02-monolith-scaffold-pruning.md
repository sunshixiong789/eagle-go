# 单体开发脚手架瘦身实施计划

## 目标

按已批准设计删除文件业务和非脚手架组件，保留 `access` 安全底座与 `dictionary` CRUD 示例，并将数据库迁移压缩为无历史兼容负担的基线。

## 执行步骤

1. **建立删除引用清单**
   - 检索 `file` API、模块、配置、Wire、Compose、文档、测试和依赖引用。
   - 阅读全部迁移与 Ent Schema，确定保留表、索引、约束和种子。

2. **移除文件业务源文件**
   - 删除 `api/eagle/file/v1`、`internal/file` 和 file Ent Schema。
   - 从配置 Proto、默认配置、Wire 组合根、错误映射和 HTTP 过滤器中删除文件能力。
   - 从 Compose 和 Makefile 中删除 MinIO、文件变量及文件权限。

3. **重建数据基线**
   - 用单个 `migrations/00001_baseline.sql` 替换全部历史迁移。
   - 保留 access、Casbin 和 dictionary 当前结构、约束、触发器及必要种子。
   - 更新迁移测试，使其验证基线当前态而非历史升级链。

4. **同步文档和依赖**
   - 更新 README、架构、本地开发部署、deploy README 和项目规则中的模块/组件说明。
   - 删除不再适用的文档与部署目录。
   - 运行生成链和 `go mod tidy`，清除 MinIO 依赖与所有生成产物引用。

5. **验证**
   - 格式化修改过的 Go 文件。
   - 运行生成稳定性检查、全仓残留引用检查、架构测试、相关包测试和全量测试。
   - 条件允许时运行 lint 与 Compose 配置校验。
   - 复核 git diff，确保没有覆盖用户无关改动。
