# Go 编码与工程规范

这份文档是本仓库的**编码规范 + 工程规范**。给人读，也给 AI 当细则。

- AI 每次会话必读的硬约束在仓库根目录 [AGENTS.md](../AGENTS.md)。
- 分层与授权边界的「为什么」在 [architecture.md](architecture.md)。
- 加接口、配权限的操作步骤在 [usage.md](usage.md)。

规范只写本仓库已经落地、并且希望后续代码继续遵守的约定。不复述 Effective Go，也不从别的语言框架搬分层。

---

## 1. 原则

1. **规则只在一个地方执行。** 不变量在 domain 构造/变更时校验；需要锁才能判断的（父节点是否存在、是否成环、是否有子节点）只在 data 事务里做。biz 不再预检一遍。
2. **有不变量才上战术 DDD。** 权限码、权限树、角色绑定是聚合/值对象。字典是 CRUD，保持贫血。不为「每个模块长得一样」硬套聚合根、仓储方法、领域事件。
3. **标准库与现有底座优先。** 切片用 `slices`，比较用 `cmp`，JSON 用 `encoding/json` 或已有的 `redisx.JSONCodec`，日志用 `log/slog`。没有调用方的预埋件（表、连接池、写路径、API）不进仓库。
4. **契约即策略。** 访问级别写在 proto 的 `access` / `perm` 上。handler 里不写鉴权 if。权限码先进入 `permission_definition`，导航节点只能引用已有码。
5. **先单体，后拆分。** 新域加在 `app/` 下同进程。只有独立扩缩、独立失败域或独立发布周期成立时才拆进程。
6. **生成代码与手写代码分开。** 手改 `*.proto`、`ent/schema/`、`db/migrations/`、`wire.go`。不要手改 `*.pb.go`、`ent/` 生成文件、`wire_gen.go`。

---

## 2. 工程规范

### 2.1 仓库形态

本仓库是 Go 模块化单体，模块路径 `github.com/eagle-go/eagle`。当前一个进程、一个 `system` 服务。

| 路径 | 职责 | 禁止 |
|---|---|---|
| `api/` | 对外契约（proto） | 业务逻辑、数据库类型 |
| `app/<svc>/internal/domain` | 模型、规则、仓储接口 | Kratos / proto / Ent / Redis / Casbin |
| `app/<svc>/internal/biz` | 跨聚合用例编排 | HTTP/gRPC 错误、Ent、proto |
| `app/<svc>/internal/data` | 仓储实现、事务、缓存、外部适配 | 对外错误契约 |
| `app/<svc>/internal/service` | proto ↔ domain，领域错误 → 传输错误 | SQL、Casbin 直调 |
| `app/<svc>/internal/server` | HTTP/gRPC 装配、中间件链 | 业务规则 |
| `pkg/` | 无业务语义的技术能力 | 反向依赖任何 `app` 包 |
| `ent/schema/` | 表结构（手写） | 在生成文件里改 schema |
| `db/migrations/` | 权威 DDL（goose） | 用 ent 自动迁移当生产路径 |

依赖方向固定：

```
transport/service → biz → domain ← data
```

由 `app/system/internal/architecture/dependencies_test.go` 在 CI 检查。改分层之前先改测试，而不是先破坏再解释。

### 2.2 加一个后端能力的固定顺序

按这个顺序做，不要跳步、不要反过来：

1. 改 `api/eagle/<svc>/v1/*.proto`：RPC、字段、`access`、`perm`、protovalidate。
2. 若是新权限码：写入 `permission_definition`（改种子或新 goose 迁移）。启动校验依赖这一步。
3. `make api`（或 `buf generate --template buf.gen.yaml`）。
4. 若改了配置 proto：`make config`。
5. domain：不变量、错误、仓储接口（接口方法必须有调用方）。
6. data：实现仓储；需要锁/事务的检查放这里。
7. biz：只编排「构造 → 调仓储」。不要复制 data 已做的检查。
8. service：映射请求/响应；新领域错误补进 `ErrorMapping`。
9. 若新增了 `NewXxx`：写入对应层的 `ProviderSet`，再 `make wire`。
10. 测试：domain 表驱动；data 走 embedded-postgres；鉴权相关补 e2e。
11. 可选：在导航树上挂按钮。挂树不会发明新契约。

加一整块 CRUD 时，先抄字典模块（贫血 + 薄 usecase），不要抄权限模块。

### 2.3 新增服务

新服务必须自带 `app/<svc>/internal/{domain,biz,data,service}`，只通过 API 契约或事件交换数据。

- 禁止跨服务 import 对方的 `internal`。
- 禁止直接读写对方拥有的表。表所有权在所属服务的架构说明里唯一声明。
- 迁移可以同一条流水线执行。
- 默认同进程；不要为「以后可能拆」先上消息队列、outbox、独立库。

新能力先问：这是所有服务都会用的技术原语，还是某个域的业务？前者进 `pkg`，后者进 `app/<svc>`。

### 2.4 契约（proto）

- 对外接口先改 proto，再写 Go。不要先写 handler 再补契约。
- 每个 RPC 必须声明 `access`。漏写则服务启动失败（故意的）。
- 需要细粒度授权时同时声明 `perm`，码必须是 `domain:resource:action` 三段，例如 `system:dict:add`。
- 校验规则写在 proto（protovalidate），不要在 service 里再写一遍同样的必填/长度检查。
- 错误原因加在 `error_reason.proto` 的 `ErrorReason`，再在 service 的 `ErrorMapping` 挂上。不要引入 `protoc-gen-go-errors`。
- 破坏性契约变更会被 `buf breaking` 拦住。要改字段语义就新增字段，不要复用旧号。

权限码合法形态只有两种：

- 具体码：严格三段，`system:dict:add`
- 通配：只能末段是 `*`，`system:dict:*`、`system:*`

`system:*:add`、`sys*:user:add`、`keyMatch2` 一律禁止。Casbin 的 `keyMatch` 只看第一个 `*` 做前缀匹配，中间带星会被当成整个前缀放行。

### 2.5 身份与授权

- 用户、口令、角色归属只在 Keycloak。本库不建用户表，不存「这个人是什么角色」。
- 本库只存「角色 → 权限」和角色继承。
- 主体从 `pkg/identity` 取：`identity.FromContext(ctx)`。不要自己解析 JWT，不要发明第二套 Principal。
- 超管短路只认 `resource_access.<client_id>.roles` 里的本服务 client role。realm 同名角色不能跨服务扩权。
- Casbin 适配器只负责加载。策略写入走 `Replace*IfVersion` / `Add*IfVersion` + 同事务 version++ + 审计。禁止 `SavePolicy` / `AddPolicy` 旁路写入。
- 多副本：事务提交后 `ReloadPolicy` + Redis 通知，周期对账兜底。不要为单库策略同步再加消息队列或 outbox。

### 2.6 数据、Ent、迁移

- **data 是服务内唯一允许 import `ent` 的包。**
- 改表：先改 `ent/schema/*.go`，再手写 `db/migrations/NNNNN_*.sql`，然后 `go generate ./ent`。
- 生产建表/改表只认 goose。禁止把 `ent/migrate` 当生产路径。
- 迁移必须可回滚。CI 会跑 `up → down-to 0 → up`。`down` 不能是空操作（除非变更本身不可逆，并在 SQL 注释里写明原因）。
- 并发：权限树用 revision，策略用 version。调用方带期望版本，冲突返回 `domain.ErrConcurrentModification`。
- 根节点 parent 用 `0` 而不是 SQL NULL：根是领域概念。
- Redis 共用一个连接池：token 撤销、字典缓存、策略 pub/sub。不要为其中一个再开 Client。
- 缓存编解码用 `redisx.JSONCodec[T]`，不要在 data 里再写一份 json codec。

### 2.7 依赖、生成、CI

允许继续用的栈：Go 1.26、Kratos v3、ent、buf、protovalidate、goose、pgx、go-redis/v9、Casbin v2、wire、OpenTelemetry、embedded-postgres、miniredis。

引入新依赖之前必须同时满足：

- 标准库或本仓库已有包做不到；
- 是该领域的权威实现（不要用工具箱库代替 `slices`/`maps`）；
- 有至少一处真实调用方。

`TestBannedDependencies` 会拒绝把 `lo` / `copier` / `mapstructure` / `pkg/errors` / Kratos v2 / zap / logrus / gorm 等写成 **直接依赖**。传递依赖不必、也不能清掉。

本地常用命令：

```bash
make api          # 对外契约
make config       # 内部配置 proto
make wire         # 依赖注入
go generate ./ent # Ent 生成
make lint         # golangci-lint
make test         # go test -race -cover ./...
make lint-proto   # buf lint + breaking
```

CI 还会检查：生成代码与 proto 同步、`go vet`、迁移往返、架构依赖方向。

日志用标准库 `log/slog`。不要引入 zap / logrus，也不要写 Kratos v2 的 `log.Helper`。网上 Kratos 教程默认是 v2，不要照抄。

### 2.8 测试分层

| 位置 | 测什么 | 依赖 |
|---|---|---|
| `domain/` | 不变量、权限码、防环、覆盖关系 | 无 |
| `pkg/authz/` | Casbin 与 `PermissionCode.Covers` 对齐、中间件 | 内存 Casbin |
| `data/` | 真实 SQL、事务、乐观锁、缓存 | embedded-postgres + miniredis |
| `service/` | 错误映射 | 无 |
| `architecture/` | 层依赖、禁依赖 | `go list` |
| `e2e/` | 401 / 403 / 200 | 进程内 HTTP + 自签 JWT |

- 单测/集成测试不启 Docker。
- e2e 必须走真实验签，禁止塞假 `Principal` 绕过 `authn`。
- 表驱动优先。比较错误用 `errors.Is`。
- 领域层与 Casbin 各有一份匹配实现，必须有对照测试（现有 `TestDomainCoversMatchesCasbinEnforcement`）。改一边必改另一边。
- 仓储接口里删掉的方法，测试改测真正的对外行为（例如「有子节点不能删」测 `Delete`，不要为测试保留 `CountChildren`）。

---

## 3. 编码规范

### 3.1 命名

- 包名：短、小写、单数。`domain`、`biz`、`data`、`authz`。不要 `models`、`utils`、`helpers`、`common`。
- 导出类型用业务词：`Permission`、`RoleBinding`、`PermissionCode`。不要 `PermissionDO`、`PermissionDTO`、`PermissionEntity`。
- 传输对象只活在 `api/` 生成代码和 `service` 的转换函数里。不要在 domain/biz 出现 `XxxDTO`。
- 接收器 1～2 个字母：`p *Permission`、`uc *PermissionUsecase`、`r *permissionRepo`。
- 文件按概念切：`permission.go`、`permission_tree.go`、`permissioncode.go`。不要 `types.go`、`consts.go`、`utils.go`。
- 测试：`TestTypeMethod_场景`，中文场景名可以，与现有 domain 测试一致。
- 错误变量：`Err` + 名词，`var ErrPermissionNotFound = errors.New("domain: 权限不存在")`。

### 3.2 错误

- domain / biz 只用标准库 `error`。禁止在这两层 import Kratos `errors`。
- 哨兵错误集中在 `domain/errors.go`。包装用 `fmt.Errorf("...: %w", err)`。
- 判断用 `errors.Is` / `errors.As`，不用 `==`，也不用 `pkg/errors`。
- HTTP/gRPC 状态码和 `ErrorReason` 只在 `service` 的 `ErrorMapping` 出现。
- data 把 Ent / SQL 错误译成领域错误（`isNotFound` → `ErrXxxNotFound`，唯一约束 → `ErrXxxDuplicated`）。不要把 `ent.NotFound` 漏到 service。
- 失败时不要丢上下文：`fmt.Errorf("get permission %d: %w", id, err)`。
- 不要 panic，除非是 `MustXxx` 且仅用于测试或包级常量。

### 3.3 注释

- 注释只解释**非显而易见的约束**：为什么通配只能在末段、为什么重建不走 `NewPermission`、为什么字典保持贫血。
- 禁止复述函数名的注释（`// ID 返回 ID`）。`revive` 的 `exported` 规则已关闭，不要为过 lint 补空话。
- 包注释写职责和依赖方向，不写框架介绍。
- 不留「下一步做 XXX」的占位注释。不做的代码就不要提交。

### 3.4 标准库优先

优先用，不要手写或引库替代：

| 需求 | 用这个 |
|---|---|
| 排序 / 查找 / 去重辅助 | `slices`（`SortFunc`、`ContainsFunc`、`Sorted`） |
| map 的键列表 | `maps.Keys` |
| 有序比较 | `cmp.Compare` |
| JSON | `encoding/json`；Redis 场景用 `redisx.JSONCodec[T]` |
| 错误 | `errors`、`fmt.Errorf` |
| 日志 | `log/slog` |
| 集合是否包含 | `slices.Contains` / `map[T]struct{}` |
| 并发单飞 | `golang.org/x/sync/singleflight`（已在 redisx） |

禁止为了少写几行 for 循环引入 `samber/lo`、`lancet`、`copier`、`mapstructure`、`cast`。

需要把 A 转成 B 时，写一个只有本层知道两边类型的小函数（`toProtoPermission`、`toDomainPermission`）。不要上通用 mapper。

### 3.5 类型与不变量

**值对象**（不可变、相等由值决定、非法值构造不出来）只用于「写错会静默失败」的概念。当前：`PermissionCode`。角色用 `type Role string` 加 `NewRole`，不要再包一层 struct。

**聚合根**字段保持私有，经构造函数 / `Update` 变更，用访问器读取。不要为了少写 getter 而导出字段——不变量会立刻失去唯一入口。从存储重建用 `XxxSnapshot` + `RehydrateXxx`，不要 14 个位置参数，也不要走会拒绝历史脏数据的 `NewXxx`。

**贫血结构**用于没有不变量的 CRUD。字段导出，data 直接填。字典就是样板。

跨多个实体才能判断的规则（防环、可见菜单、已知码集合）放领域服务或只读视图，例如 `PermissionTree`。不要塞进单个实体，也不要放到 biz 里用循环手写一遍。

### 3.6 接口

- 接口由**调用方**定义，放在 domain。实现放在 data。
- 方法必须有生产调用方。测试需要的行为，测对外方法（`Delete`、`ResolveCodes`、`ListBindings`），不要为测试在接口上留 `Allow`、`CountChildren`、`ListBoundRoles`。
- 不要写只包含一个方法、却只被一个实现满足的前缀接口（`type PermissionCreator interface { Create... }`），除非有第二个实现。
- 小接口可以保留在使用处；仓储接口按聚合切，不要一个 `Repository` 通吃全站。

### 3.7 控制流与 API 形状

- `context.Context` 是第一个参数。不要塞进结构体字段（除了生命周期明确的后台 goroutine）。
- 错误是最后一个返回值。不要用 panic 当控制流。
- 接收器：类型需要不变量或身份时用指针；纯值对象的只读方法用值接收器。
- 零值有意义就用零值（空权限码 = 目录/菜单不必挂码）。不要用指针表示「没有」除非三态必须。
- 函数参数超过 4 个、且同类型容易传错时，收成 `XxxParams` 结构体。
- 直接返回，不要包一层什么都不做的函数。

```go
// 好
return uc.repo.GetByID(ctx, id)

// 坏
func (uc *Usecase) Get(ctx context.Context, id int64) (*Foo, error) {
    foo, err := uc.repo.GetByID(ctx, id)
    if err != nil {
        return nil, err
    }
    return foo, nil
}
```

- 同一段收尾逻辑出现两次以上就抽出来（例如 `commitPolicy`：映射并发/环错误 → Reload → Notify）。不要为「保持对称」复制粘贴。
- biz 里不要再做 data 事务中已经做的存在性/环/子节点检查。那是 TOCTOU，多一次查询还可能得出相反结论。

### 3.8 格式

- `gofmt` + `goimports`。本模块前缀：`github.com/eagle-go/eagle`（第三方与本模块之间空一行）。
- 缩进 tab，行宽按 `gofmt` 默认，不要手排。
- 不用下划线导入，除非实现接口的副作用（几乎用不到）。
- 导出标识符的中文注释可以，但必须提供信息。

---

## 4. 分层细则

### domain

放：实体、值对象、领域错误、仓储接口、需要全图才能判断的规则（`PermissionTree`）。

不放：Kratos、protobuf、Ent、Redis、Casbin、HTTP 状态码、日志、指标。

```go
// 好：构造即校验
perm, err := domain.NewPermission(params)

// 坏：先 new 空对象再 setter，校验散落各处
p := &domain.Permission{}
p.SetName(name)
```

### biz

放：跨聚合步骤、决定调用哪个仓储、把角色字符串收成 `[]domain.Role`。

不放：SQL、缓存键、Kratos 错误、proto 类型、与 data 重复的预检。

```go
// 好：构造后交给持锁的仓储
func (uc *PermissionUsecase) CreatePermission(ctx context.Context, params domain.NewPermissionParams) (*domain.Permission, error) {
    perm, err := domain.NewPermission(params)
    if err != nil {
        return nil, err
    }
    return uc.repo.Create(ctx, perm)
}

// 坏：biz 先查父节点、再查环，data 进锁后又查一遍
```

### data

放：Ent 读写、事务、乐观锁、Ent 错误翻译、缓存、Casbin 适配。

不放：对外 reason 码、业务校验的「另一份实现」（应调用 domain）。

从行到实体：`RehydratePermission(PermissionSnapshot{...})`。不要把 Ent 类型漏出 data 包。

### service

放：`req` → domain 参数、domain → proto、`ErrorMapping`。

不放：鉴权 if、直接打数据库、重新实现 `PermissionCode` 规则。

当前登录人：

```go
p, ok := identity.FromContext(ctx)
if !ok {
    return nil, /* 让中间件处理未登录；已登录接口不应走到这里 */
}
```

### pkg

只放验签、鉴权中间件、DB/Redis 客户端、健康检查、身份 context、观测。

禁止：菜单树、字典、业务校验、import `app/...`。

---

## 5. AI / 贡献者禁止清单

这些是本仓库已经明确拒绝、且生成代码时最容易再引进来的东西：

| 不要做 | 原因 |
|---|---|
| 把四层收成 `handler + repo` | 架构测试和拆服务边界依赖现有分层 |
| 给 `Permission` 导出字段以去掉 getter | 不变量会失去唯一入口 |
| 替换 Casbin，或改用 `keyMatch2` | `keyMatch2` 会把 `:xxx` 当路径参数，只读角色变全体写权限 |
| 引入 `lo` / `copier` / `mapstructure` | 标准库够用；通用 mapper 会绕过不变量 |
| 为字典写聚合根、值对象、领域事件 | 没有不变量，纯仪式 |
| 在 biz 复制 data 的存在性/环/子节点检查 | TOCTOU，而且是双份规则 |
| 在仓储接口留「测试专用」方法 | 测试应打真实行为 |
| 在 handler 里写 `if 没权限` | 权限在 proto，中间件判定 |
| 本地再建用户表 / 用户角色表 | 与 Keycloak 双写 |
| 管理端先建菜单再发明权限码 | 契约目录才是权威 |
| 为策略同步加 outbox / MQ | Redis 通知 + 对账已经足够 |
| 手改 `*.pb.go` / `ent/*.go` / `wire_gen.go` | 下一轮 generate 会盖掉 |
| 照抄 Kratos v2 教程（`log.Helper`、旧 contrib 路径） | 本仓库是 v3 + slog |
| 预埋没有调用方的表、连接、API | 底座不养死代码 |

---

## 6. 规范怎么改

约定变了，按这个顺序改，避免文档骗人：

1. 先改代码和（如有）架构测试，让新约定可执行。
2. 改 [AGENTS.md](../AGENTS.md) 的对应硬约束。
3. 改本文的原则或细则，补一条正反例。
4. 若影响加功能步骤，再改 [usage.md](usage.md) / [architecture.md](architecture.md)。

不要只改文档不改测试，也不要只在对话里「约定一下」却不落盘。
