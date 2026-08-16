# eagle-go 生成约束

本文件是 AI 的硬约束，每次改代码都必须遵守。细则和正反例：[docs/conventions.md](docs/conventions.md)。加接口步骤：[docs/usage.md](docs/usage.md)。分层理由：[docs/architecture.md](docs/architecture.md)。

不要复述本文去「优化可读性」。不要从 Java / Spring / 其它语言框架搬分层、starter、DTO 体系。

## 开工前

1. 先在本仓库找一个同类模块抄形状：CRUD 抄字典，有不变量抄权限/角色绑定。
2. 不确定落点就读 `docs/conventions.md`，不要发明新层、新工具包、新框架。
3. 只改任务需要的文件。不顺手重构、不补无关注释、不升级无关依赖。

## 架构（不可破）

依赖方向：`transport/service → biz → domain ← data`。

- `domain` 禁止 import：`github.com/eagle-go/eagle/api/`、`github.com/go-kratos/`、`entgo.io/`、`github.com/eagle-go/eagle/ent`、`github.com/redis/`、`github.com/casbin/`。
- `biz` 禁止 import：`api/`、`ent`、Kratos、Redis、Casbin。
- 服务内只有 `data` 可以 import `ent`。
- `pkg/` 禁止 import 任何 `app/` 包。
- 这些由 `app/system/internal/architecture/dependencies_test.go` 检查；改分层先改测试。

禁止：

- 把四层收成 handler + repo。
- 给 `Permission` / `RoleBinding` / `PermissionCode` 导出字段来去掉 getter。
- 替换 Casbin，或把 matcher 改成 `keyMatch2`。
- 新建用户表、用户角色表、口令存储。
- 为策略同步加 outbox / 消息队列。
- 预埋没有调用方的表、连接池、API、接口方法。

## 分层落点

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

- 有不变量才写聚合根。字典保持贫血结构体 + 薄 usecase。
- biz 禁止再做 data 事务里已做的检查（父节点是否存在、是否成环、是否有子节点）。那是 TOCTOU。
- 需要锁才能判断的规则只放 data；纯值规则只放 domain。
- 仓储接口的每个方法必须有生产调用方。测试打 `Delete` / `ResolveCodes` / `ListBindings`，不要为测试保留 `CountChildren`、`Allow`、`ListBoundRoles`。
- data 重建实体用 `XxxSnapshot` + `RehydrateXxx`，不要超长位置参数，也不要用会拒绝历史数据的 `NewXxx`。
- 同一段写后收尾出现两次就抽函数（参考 `commitPolicy`、`withTreeTx`）。
- 当前用户用 `pkg/identity.FromContext`。不要自己解析 JWT。

## 契约、权限、身份

加后端接口的顺序不可颠倒：

1. 改 proto（HTTP 注解 + `access` + 需要时 `perm` + protovalidate）
2. 新权限码写入 `permission_definition`（种子或 goose 迁移）
3. `make api`（配置 proto 再 `make config`）
4. domain → data → biz → service（新错误挂 `ErrorMapping`）
5. 新构造函数进 `ProviderSet` 后 `make wire`
6. 测试
7. 可选：导航树挂已有码

- 每个 RPC 必须有 `access`。handler 里禁止写鉴权 if。
- 权限码：具体码严格三段 `domain:resource:action`；通配只能末段 `*`。禁止 `system:*:add`、`sys*:x:y`。
- 导航节点只能引用目录里已启用的具体码。创建菜单不能发明新契约。
- 角色在 Keycloak，本库只存角色→权限和角色继承。
- 策略写入必须走 `Replace*IfVersion` / `Add*IfVersion`（同事务 version++ 与审计）。禁止 `SavePolicy` / `AddPolicy`。
- 超管只认本服务 client role，不认 realm 同名角色。

## Go 写法

- 标准库优先：`slices`、`maps`、`cmp`、`encoding/json`、`errors`、`fmt.Errorf("%w")`、`log/slog`。
- Redis JSON 用已有 `redisx.JSONCodec[T]`，不要再写 codec。
- 禁止新增：`samber/lo`、`lancet`、`jinzhu/copier`、`mapstructure`、`spf13/cast`、`pkg/errors`、`logrus`、`zap`、`gorm`、Kratos v2。
- 类型映射写本层小函数（`toProtoPermission` / `toDomainPermission`），不上通用 mapper。
- domain/biz 只用标准库 `error`。Kratos 错误和 `ErrorReason` 只出现在 service。
- 判断错误用 `errors.Is` / `errors.As`。
- 注释只写非显而易见的约束。禁止 `// ID 返回 ID`。`revive` 的 `exported` 已关，不要为过 lint 补空话。
- 能直接 `return repo.X(...)` 就不要包一层 if err。
- 参数 ≥ 4 个且易传错时用 `XxxParams`。角色用 `type Role string`，不要再套 struct。
- 日志用 `log/slog`。不要抄网上 Kratos v2 的 `log.Helper`。
- 手改 `*.proto`、`ent/schema`、`db/migrations`、`wire.go`。禁止手改 `*.pb.go`、`ent/` 生成文件、`wire_gen.go`。
- 包名不要 `utils` / `common` / `helpers` / `models`。不要 `XxxDTO` / `XxxDO` 进 domain。

## 数据与迁移

- 改表：`ent/schema` → 手写 `db/migrations/NNNNN_*.sql` → `go generate ./ent`。生产只认 goose，不用 ent 自动迁移。
- 迁移必须可回滚（CI 会 `up → down-to 0 → up`）。
- 权限树 revision、策略 version：冲突返回 `domain.ErrConcurrentModification`。
- Redis 共用现有连接池，不要为缓存/通知再开 Client。

## 测试

- domain：表驱动，零依赖。
- data：embedded-postgres + miniredis，不启 Docker。
- e2e：真实验签的 JWT，覆盖 401/403/200。禁止塞假 Principal 绕过 authn。
- 改 `PermissionCode.Covers` 或 Casbin matcher 必须跑两边对照测试。
- 架构测试必须保持绿色。
- 声称测过必须有命令输出。至少：`gofmt` 所改文件，以及相关包的 `go test`。

## 完成前自检

- [ ] 依赖方向没破；没有新的禁依赖。
- [ ] 没有在 biz 复制 data 的锁内检查。
- [ ] 没有新增无调用方的接口方法。
- [ ] 新 RPC 有 `access`（及需要的 `perm`），新码已进 `permission_definition`。
- [ ] 新领域错误已进 `ErrorMapping`。
- [ ] 生成代码已更新（proto / ent / wire）。
- [ ] 没手改生成文件，没加 lo/copier/mapstructure，没写鉴权 if，没建用户表。
- [ ] 注释没有复述代码，没有占位 TODO。
