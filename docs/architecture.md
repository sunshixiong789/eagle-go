# Eagle 架构边界

本仓库是 Go 模块化单体：当前一个进程、一个 `system` 服务，按业务域保留可拆边界。不移植其它语言的框架分层或 starter 体系。

服务内依赖方向固定为：

`transport/service → biz → domain ← data`

- `domain` 只包含领域模型、规则和仓储接口，不依赖 Kratos、protobuf、Ent、Redis 或 Casbin。
- `biz` 负责编排跨聚合用例，不处理 HTTP/gRPC 错误。
- `service` 是传输适配层，统一把领域错误映射为外部错误契约。
- `data` 是唯一允许直接依赖 Ent 的服务内包，负责事务、并发控制、缓存和外部系统适配。
- `pkg` 只放无业务语义的技术能力（验签、鉴权中间件、DB/Redis 客户端、健康检查、身份 context），禁止反向依赖任何 `app` 包。

这些规则由 `app/system/internal/architecture/dependencies_test.go` 在 CI 中检查。

## 底座怎么长

底座只提供业务服务反复会用到的能力，新域直接复用，不复制一份：

| 层 | 放什么 | 不放什么 |
|---|---|---|
| `pkg/authn` + `pkg/identity` | OIDC 验签、主体进 context | 用户表、口令、角色归属 |
| `pkg/authz` | proto 注解、中间件、Casbin 判定 | 菜单树、字典、业务校验 |
| `pkg/db` / `pkg/redisx` / `pkg/healthx` / `pkg/otelx` | 连接、缓存原语、探活、链路 | 业务仓储 |
| `app/<service>` | 该域的用例与表 | 其它服务的 internal 或表 |

原则：

1. **先单体，后拆分。** 新域先加在本仓库 `app/` 下同进程；只有独立扩缩、独立失败域或独立发布周期成立时才拆进程。
2. **有不变量才进 domain。** 权限码、权限树、角色绑定有规则要守；字典是 CRUD，保持贫血，不为统一骨架硬套聚合根。
3. **契约即策略。** 访问级别写在 proto 的 `access` / `perm` 上，启动时校验，运行时 fail-closed。handler 里不写鉴权 if。
4. **身份外置，授权内聚。** Keycloak 回答「谁、有哪些角色」；本库只存「角色 → 权限」和角色继承，本地 JWKS 验签，不回调 IdP。
5. **策略同步保持简单。** 事务内 version++ 与审计，Redis 通知其它副本，周期对账兜底。不为单库策略同步再加消息队列或 outbox。
6. **测试即基础设施。** 单测不启 Docker：embedded-postgres 跑真实迁移，miniredis 替 Redis，自签 JWT 替 Keycloak。e2e 覆盖 401/403/200，不拿假 Principal 绕过验签。

新增能力之前先问：这是所有服务都会用的技术原语，还是某个域的业务？前者进 `pkg`，后者进对应 `app/<service>`。没有调用方的预埋件（表、连接池、写路径、API）不进底座。

## 数据与授权边界

- `permission_definition` 是后端 RPC 权限目录；`navigation_node` 是前端导航。隐藏或删除导航不会删除后端授权契约。
- 用户与角色归属由 Keycloak 维护，本库只保存“角色 → 权限”和角色继承，避免双写用户角色数据。
- 超管短路只认 `resource_access.<client_id>.roles` 中的本服务 client role；realm 同名角色不能跨服务扩权。
- 策略写入、版本递增和审计在同一数据库事务提交；各副本经 Redis 通知并由版本对账兜底。
- 权限树和策略写入分别使用单调 revision/version 做乐观并发控制。

## 服务拆分规则

新增服务时必须拥有自己的 `app/<service>/internal/{domain,biz,data,service}`，只通过 API 契约或事件交换数据。禁止跨服务直接导入对方的 `internal` 包，也禁止直接读写对方拥有的表。迁移仍可由同一流水线执行，但每张表的所有权必须在所属服务的架构文档中唯一声明。
