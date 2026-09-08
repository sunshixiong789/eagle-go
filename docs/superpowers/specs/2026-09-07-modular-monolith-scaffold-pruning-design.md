# C 端模块化单体脚手架精准瘦身设计

## 背景

`eagle-go` 是尚未投入生产的公司级 Go 后端脚手架，服务国内外 C 端项目。当前仓库已经具备
Google/Apple 社会化登录、本地会话与 Eagle token、运营后台 RBAC、字典 CRUD、PostgreSQL、
可观测性和部署资产，但最近从外部 OIDC 资源服务器演进到自有会话后，旧兼容链仍散落在配置、
中间件、测试、迁移和文档中。仓库还保留了若干仅为历史兼容或假想未来能力存在的字段、抽象和依赖。

本次采用精准瘦身，而不是更换框架或提前搭建微服务基础设施。目标是让当前单体更小、更一致，
同时通过真实模块边界而非闲置依赖保留未来拆分能力。

## 目标

- 保持一个可直接运行、可复制的模块化单体脚手架。
- 面向国内外 C 端项目，保留 Google/Apple 登录，并为未来手机号登录保留最小领域端口。
- 保留运营后台所需的权限目录、角色授权、Casbin 判定、审计和多副本策略同步。
- 删除外部 OIDC access token 兼容链、旧角色模型、历史兼容字段、无调用抽象和过期文档。
- 仅保留有当前生产、测试或生成用途的依赖；标准库能清楚解决的问题不引入辅助库。
- 允许重建迁移基线和调整尚未对外使用的 API/配置，不承担生产兼容成本。

## 非目标

- 本次不实现手机号验证码登录、微信登录、账户合并或多身份绑定。
- 不将单体拆成微服务，也不加入内部 gRPC、服务发现、消息队列、缓存、配置中心或分布式事务。
- 不把认证、RBAC 或观测拆成多套可选模板。
- 不用 `net/http`、手写 SQL 或自研权限引擎替换现有成熟技术栈。
- 不为了缩短 `go.mod` 而重写密码学、Protobuf 校验、ORM、迁移或遥测能力。
- 不进行与删除冗余和统一边界无关的目录美化式重构。

## 目标架构

仓库继续保持一个 Go module、一个 `eagle` 二进制、一个 PostgreSQL 数据库和一个镜像：

```text
cmd/eagle                         显式组合根
internal/auth                     C 端身份、社会化登录、会话、Eagle token
internal/access                   运营后台 RBAC、权限目录、策略同步
internal/dictionary               唯一的简单 CRUD 示例
internal/platform/database        共享 Ent Client
pkg/platform + pkg/*              无业务语义的 HTTP、配置、观测和身份技术能力
```

模块内继续按复杂度选择最小依赖形态：

```text
interfaces -> domain <- infrastructure

或

interfaces -> application -> domain <- infrastructure
```

模块之间不得 import 对方的 `interfaces` 或 `infrastructure`。跨模块用例由消费模块声明最小端口，
组合根注入实现。当前不为这些端口增加网络语义；未来某个模块出现独立发布、扩缩容或故障隔离需求时，
再将同一端口适配为 HTTP/gRPC 调用。

## 认证与身份

系统只保留一条 access token 主线：

```text
Google/Apple ID Token
        ↓
auth ProviderVerifier
        ↓
本地 Identity + Session
        ↓
Eagle JWT
        ↓
authn 中间件 -> Principal
        ↓
authz 中间件 -> Casbin
```

Google/Apple 只证明第三方身份。`auth` 创建或更新本地身份、创建可撤销会话，并签发 Eagle JWT。
Eagle JWT 使用 HS256，至少包含 `sub`、`roles`、`iss`、`aud`、`iat` 和 `exp`。refresh token 继续使用
密码学随机值，数据库只保存 SHA-256 哈希，每次刷新轮换，退出时撤销。

`pkg/authn` 只验证 Eagle JWT，不再兼容外部 OIDC/JWKS access token。删除动态 claim 路径后，
`roles` 是固定字符串数组。`Principal` 只保留当前授权和审计真实使用的主体字段；删除只服务旧 IdP
模型的 `Scopes`、`ClientRoles` 和固定 `ClientID`。

`auth/domain.ProviderVerifier` 继续作为第三方身份验证的最小端口，现有 infrastructure 提供
Google/Apple 适配器。未来增加手机号登录时扩展 Provider 与 adapter；本次不预建短信客户端、表、API
或依赖。

## 角色与授权

角色使用普通稳定键，例如 `user`、`admin`。删除 `realm:` / `client:` 命名空间及动态 claim 映射。
本地身份当前仍只有一个角色，但 JWT 使用 `roles` 数组，授权中间件始终按角色集合判定。

删除 `super_admin_role` 配置和代码短路。管理员权限作为普通 Casbin 策略保存：

```text
admin -> system:*
```

因此所有授权都经过同一判定、版本和审计链路。Casbin 仍是 infrastructure 选择；domain 和 interfaces
只依赖窄接口，不 import Casbin。

以下当前能力继续保留：

- Proto 上显式声明 `access`，需要细粒度权限时同时声明 `perm`。
- 权限目录与导航节点分离，导航只能引用已定义权限码。
- 角色权限与角色继承的事务写入、乐观版本和追加式审计。
- 多副本每 5 秒对账策略版本，落后或读取失败时 readiness fail closed。
- 启动时核对 Proto 权限声明与数据库权限目录。

## API 与配置

保留现有登录、刷新、退出、权限管理、角色授权和字典 CRUD 的 HTTP 路径及主要响应结构。
`SocialLogin` 仍只接受 Google/Apple。角色授权 API 改用 `admin`、`user` 这类角色键。

方法访问控制只保留：

- `ACCESS_LEVEL_PUBLIC`
- `ACCESS_LEVEL_AUTHENTICATED`
- `ACCESS_LEVEL_PERMISSION_REQUIRED`，并要求非空 `perm`

删除仅为旧契约兼容存在的 `public` 方法扩展及解析分支。RPC 未声明有效 `access` 时启动和请求判定
继续 fail closed。

`Auth` 配置收敛为 Eagle token 和社会化登录实际需要的字段：

- `issuer`
- `audience`
- `signing_secret`
- `access_token_ttl`
- `refresh_token_ttl`
- `google.enabled` / `google.client_id`
- `apple.enabled` / `apple.client_id`

删除 `jwks_url`、`jwks_path`、`realm_roles_claim`、`client_roles_claim`、固定 `client_id` 和
`super_admin_role`。配置校验不再使用只有一个真实组合的 `config.Requirements`；应用启动统一校验完整
Bootstrap。

## 数据与迁移

仓库尚未投产，本次重建单一迁移基线：

- 将 `social_identity` 与 `auth_session` 合并进 `00001_baseline.sql`。
- 删除 `00002_social_auth.sql`。
- Casbin 表只保留当前 RBAC 使用的 `ptype`、`v0`、`v1`，删除为假想 ABAC 预留的 `v2`～`v5`。
- `authz_policy_audit` 删除没有真实来源的 `actor_client_id`。
- 初始角色策略改为普通 `admin` / `user` 键。
- Ent Schema 与 goose SQL 同步修改，并通过生成命令重建 Ent 代码。

保留 permission tree revision、policy version、授权审计、refresh token 哈希与索引。这些字段服务当前
并发、安全或多副本行为，不属于未来预留。

## 依赖策略

保留当前有真实调用方且承担明确基础能力的成熟依赖：

- Kratos/Protobuf/Buf/Protovalidate：HTTP 契约、生成和入站校验。
- Ent/pgx/goose：PostgreSQL 模型、驱动和生产迁移。
- Casbin：后台 RBAC 判定和角色继承。
- `go-oidc`：Google/Apple ID Token 的 OIDC/JWKS 验证。
- `go-jose`：Eagle JWT 签发与验证。
- OpenTelemetry/Prometheus：trace、metrics 和标准化导出。
- embedded-postgres：无需 Docker 的真实 PostgreSQL 集成测试。

生成器和开发工具继续隔离在 `tools/go.mod`，避免污染业务依赖图。业务 `go.mod` 中的测试依赖只要
被真实测试调用就允许存在；不通过将测试迁出可发现范围来制造更短的依赖清单。

删除 `go.uber.org/automaxprocs`。项目使用 Go 1.27，而 Go 1.25 起运行时已经默认读取 Linux cgroup
CPU 限额并动态更新 `GOMAXPROCS`，该依赖已重复标准库能力。

完成源代码删除后对根 module 和 tools module 执行 `go mod tidy`。不得为了“未来可能使用”保留没有
当前 import 的 module；未来能力由端口和模块所有权保留，需要时再加入依赖。

## 代码与文档清理

除上述业务变更外，删除以下明确冗余：

- `buildApp -> composeApp` 纯转发包装。
- 外部 OIDC resource server 的验证分支、claims 路径工具和对应测试替身。
- `public` 兼容注解和相关测试分支。
- 只服务旧角色命名的 identity helper 与 super-admin bypass。
- 配置、部署脚本和环境示例中的旧 OIDC/JWKS 变量。
- 已失效且与当前代码矛盾的旧瘦身 spec/plan。
- README、架构、登录和部署文档中的 Wire、外部 IdP 资源服务器及旧角色说明。

保留显式组合根、模块错误映射、健康/指标端点、数据库迁移工具、部署资产、数据所有权检查和架构测试。
不合并有独立职责的包，也不删除能够覆盖真实安全或事务风险的测试。

## 错误边界

- Google/Apple 验签和远端响应错误在 `auth/infrastructure` 翻译为领域错误。
- Eagle token 过期与无效映射为稳定的 401 reason，内部签名细节不返回客户端。
- 未登录返回 401；已登录但无权限返回 403。
- RPC 访问策略无效、授权判定器未装配、策略加载或读取失败时拒绝访问。
- interfaces 只做协议转换和错误边界适配，不解析 JWT、不直接调用 Casbin、不暴露 Ent/SQL 错误。

## 测试策略

行为变更按 TDD 实施：先让针对新行为的测试失败，再修改最小实现。

- `pkg/authn`：Eagle HS256 token、固定 roles、过期、错误签名、错误格式和 Bearer 解析。
- `pkg/authz`：三种 access level、缺失策略 fail closed、普通 Casbin 通配、删除超管短路后的行为。
- `pkg/platform/config`：精简后的必填字段、密钥长度、TTL 关系、provider client ID 和端口冲突。
- `internal/auth`：Google/Apple provider、会话创建、refresh 轮换、退出撤销和事务回滚。
- `internal/access`：角色键、权限集合、继承、版本、审计和策略对账。
- `tests/database`：goose 基线与 Ent 元数据一致。
- `tests/e2e`：社会化登录、Eagle token 调用受保护 API、刷新、退出和角色权限。
- `tests/architecture`：模块依赖、数据所有权、生成代码边界和拒绝依赖清单。

## 验证与完成标准

实现完成后至少执行：

```bash
make generate
make lint
go test -short ./...
make test
make validate-deploy
```

并确认：

- 修改过的 Go 文件已经格式化。
- 生成后没有源文件与生成文件不一致。
- 根 module 与 tools module 均已 tidy。
- 仓库不再引用外部 OIDC access token 模式、动态 claim 路径、旧角色命名、`public` 兼容字段或
  `automaxprocs`。
- `go.mod` 中每个直接依赖均能定位到真实生产、测试或生成调用方。
- README、架构、登录和部署文档描述同一套实际行为。
- 所有相关测试通过；因环境限制无法运行的验证项被明确记录。
