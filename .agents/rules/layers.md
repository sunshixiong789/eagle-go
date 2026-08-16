# 分层

依赖方向：`transport/service → biz → domain ← data`。由 `app/system/internal/architecture/dependencies_test.go` 检查；改分层先改测试。

| 改什么 | 落在哪 |
|---|---|
| 不变量、值对象、仓储接口、领域错误 | `internal/domain` |
| 跨聚合步骤（构造实体 → 调仓储） | `internal/biz` |
| SQL / Ent / 事务 / 锁 / 缓存 / Casbin 适配 | `internal/data` |
| proto ↔ domain、错误映射 | `internal/service` |
| 中间件链、HTTP/gRPC 装配 | `internal/server` |
| 验签、鉴权中间件、DB/Redis 客户端、身份 context | `pkg/` |
| 对外 RPC、校验、access/perm | `api/eagle/**/*.proto` |
| 表结构 | `ent/schema` + `db/migrations` |

- 有不变量才写聚合根。字典保持贫血结构体 + 薄 usecase，不要抄权限模块的仪式。
- 规则只在一处执行：纯值规则在 domain 构造/变更时校验；需要锁才能判断的（父节点、成环、子节点）只在 data 事务里做。biz 禁止再预检一遍（TOCTOU）。
- 仓储接口每个方法必须有生产调用方。测试打对外行为（`Delete`、`ResolveCodes`、`ListBindings`），不要为测试留 `CountChildren` / `Allow` / `ListBoundRoles`。
- data 重建实体用 `XxxSnapshot` + `RehydrateXxx`，不要超长位置参数，也不要用会拒绝历史脏数据的 `NewXxx`。
- 同一段写后收尾出现两次就抽函数（参考 `commitPolicy`、`withTreeTx`）。能直接 `return repo.X(...)` 就不要包一层。
- `domain` / `biz` 禁止 import：`api/`、Kratos、Ent、Redis、Casbin。服务内只有 `data` 可 import Ent。`pkg/` 禁止 import `app/`。

禁止：把四层收成 handler + repo；给 `Permission` / `RoleBinding` / `PermissionCode` 导出字段来去掉 getter。
