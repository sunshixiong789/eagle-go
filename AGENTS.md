# eagle-go 生成约束

本文件每次会话都会加载，只放**常驻硬约束**。细则按需读 `.agents/rules/`，不要一上来通读。

给人看的教程和架构说明在 `docs/`，不要把它们和规则混在一起。

不要从 Java / Spring 搬分层、starter、DTO 体系。CRUD 抄字典，有不变量抄权限/角色绑定。只改任务需要的文件。

## 按需读哪份

| 在做什么 | 先读 |
|---|---|
| 加一个接口 | [docs/usage.md](docs/usage.md) **第 4 节** |
| 加一整块 CRUD | [docs/usage.md](docs/usage.md) **第 7 节** |
| 要不要拆服务、东西进 pkg 还是 app | [docs/architecture.md](docs/architecture.md) |
| 加/改分层、仓储接口 | [.agents/rules/layers.md](.agents/rules/layers.md) |
| 改 proto、权限码、鉴权、当前用户 | [.agents/rules/api-authz.md](.agents/rules/api-authz.md) |
| 改表、迁移、Ent、文件存储、策略写入收尾 | [.agents/rules/data.md](.agents/rules/data.md) |
| 新写 Go、引入依赖、错误/注释 | [.agents/rules/style.md](.agents/rules/style.md) |
| 补测试 | [.agents/rules/testing.md](.agents/rules/testing.md) |

不要为了写代码通读 `docs/architecture.md`。

## 不可破

- 依赖：`interfaces → application → domain ← infrastructure`。`domain`/`application` 不碰 Kratos、proto、Ent、Casbin。只有模块 `infrastructure` 与 `internal/platform/database` 碰 Ent。`pkg/` 不 import `internal/`。
- 有不变量才上聚合根。字典保持贫血。application 禁止复制 infrastructure 锁内检查（父节点/成环/子节点）。
- 每个 RPC 必须有 `access`。handler 里不写鉴权 if。不建用户表。权限码先入 `permission_definition`，菜单不能发明新码。
- 禁止：收成 handler+repo；导出 `Permission` 字段去 getter；换 Casbin / 改用 `keyMatch2`；加 `lo`/`copier`/`mapstructure`；为策略同步加 outbox/MQ；手改 `*.pb.go` / `ent/` 生成文件；预埋没有调用方的接口、表、API。
- 标准库优先（`slices`/`maps`/`cmp`/`slog`）。日志不要抄 Kratos v2 的 `log.Helper`。
