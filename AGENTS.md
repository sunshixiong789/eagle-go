# eagle-go 生成约束

本文件每次会话都会加载，只放**常驻硬约束**。细则按需读 `.agents/rules/`，不要一上来通读。

给人看的上手教程在 `README.md`，架构与部署专题在 `docs/`，不要把它们和规则混在一起。

不要从 Java / Spring 搬分层、starter、DTO 体系。CRUD 抄字典，有不变量抄权限/角色绑定。只改任务需要的文件。

## 按需读哪份

| 在做什么 | 先读 |
|---|---|
| 加一个接口 | [README.md](README.md) **“新增或修改 API”** |
| 加一整块 CRUD | [README.md](README.md) **“新增业务模块或 CRUD”** |
| 服务边界、跨服务调用、东西进 pkg 还是 app | [docs/architecture.md](docs/architecture.md) |
| 加/改分层、仓储接口 | [.agents/rules/layers.md](.agents/rules/layers.md) |
| 改 proto、权限码、鉴权、当前用户 | [.agents/rules/api-authz.md](.agents/rules/api-authz.md) |
| 改表、迁移、Ent、文件存储、策略写入收尾 | [.agents/rules/data.md](.agents/rules/data.md) |
| 新写 Go、引入依赖、错误/注释 | [.agents/rules/style.md](.agents/rules/style.md) |
| 补测试 | [.agents/rules/testing.md](.agents/rules/testing.md) |

不要为了写代码通读 `docs/architecture.md`。

## 不可破

- 依赖：`service → application → domain ← infrastructure`。`domain`/`application` 不碰 Kratos、proto、Ent、Casbin。只有模块 `infrastructure` 与服务内 `internal/platform/database` 碰 Ent。`pkg/` 不 import `app/`。
- 每个 `app/<service>/cmd/<service>` 只能组合自己拥有的模块。组合根用 Wire；`google/wire` 不要进 domain/application/infrastructure。服务独占 `app/<service>/migrations` 和 database/Ent；禁止跨服务 import 实现、查表、外键或事务，跨边界只走 `api` 契约或事件。
- 有不变量才上聚合根。字典保持贫血。application 禁止复制 infrastructure 锁内检查（父节点/成环/子节点）。
- 每个 RPC 必须有 `access`。handler 里不写鉴权 if。不建用户表。权限码先入 `permission_definition`，菜单不能发明新码。
- 禁止：收成 handler+repo；导出 `Permission` 字段去 getter；换 Casbin / 改用 `keyMatch2`；加 `lo`/`copier`/`mapstructure`；为策略同步加 outbox/MQ；手改 `*.pb.go` / `ent/` / `wire_gen.go` 生成文件；预埋没有调用方的接口、表、API。
- 标准库优先（`slices`/`maps`/`cmp`/`slog`）。日志不要抄 Kratos v2 的 `log.Helper`。
