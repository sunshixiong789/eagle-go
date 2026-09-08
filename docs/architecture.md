# Eagle 架构

本仓库是**单进程单体**：一个 Go module、一个二进制、一个数据库、一个镜像。分层与模块边界依然显式存在，但它们由目录约定和架构测试保证，不再由进程和网络边界保证。

这是一个刻意的取舍。进程拆分带来的独立发布、独立扩缩容和故障隔离，代价是分布式事务、跨进程调用的失败模式、多份配置与多套部署清单。除非确实存在这些收益的需求，单体在同等业务复杂度下的正确性成本要低得多。保留清晰的模块边界，是为了将来真的出现拆分信号时，迁出的是一个已经内聚的目录，而不是一团缠绕的调用。

## 模块

| 模块 | 职责 | 拥有的数据 | 外部依赖 |
|---|---|---|---|
| `access` | 权限码目录、导航树、角色绑定、Casbin 策略 | `permission_definition`、`casbin_rule`、策略版本 | 无 |
| `auth` | 账号、Google/Apple 登录身份、Eagle token 与会话 | `user_account`、`user_identity`、`user_role_binding`、`auth_session` | Google/Apple JWKS |
| `dictionary` | 字典的简单 CRUD，作为新模块的样板 | `dict_type`、`dict_data` | 无 |

Google/Apple 只负责证明第三方身份；Eagle 用不透明 `subject` 表示统一账号，一个账号可以显式绑定多个
登录身份。Eagle 保存最小身份资料与可撤销会话，并签发自己的 access token。

## 目录表达什么

    api/eagle/<module>/v1               API 契约（proto，只生成 HTTP）
    cmd/eagle                           进程入口和唯一组合根（显式构造）
    internal/<module>                   业务模块
    internal/platform/database/ent      Ent Client 与 schema
    migrations                          goose SQL 迁移
    pkg                                 无业务语义的共享技术模块
    tools                               生成器与迁移程序（独立 go.mod）
    tests                               架构测试、e2e 与测试工具
    deploy                              本地 Compose 与云效/ECS 应用部署资产

`internal/` 是 Go 的编译器可见性边界，仓库外无法 import。仓库内的模块边界由 `tests/architecture` 检查：模块之间不得 import 对方的 `interfaces` 或 `infrastructure`。

`tools/` 保留独立 `go.mod`，是为了让 buf、goose、golangci-lint 和 protoc 插件的版本被锁定，又不进入业务依赖图。生成的二进制落在 `bin/`，由 Makefile 在仓库根目录调用——工具进程的工作目录必须是仓库根，否则 `buf.gen.yaml` 的相对路径、goose 的 `-dir`都会失效。

## 模块内渐进式分层

模块不按目录数量评价 DDD，而是按用例复杂度选择最小结构。纯 CRUD 或查询使用：

    interfaces → domain ← infrastructure

存在多端口协作、聚合加载-变更-保存、用例级事务编排、幂等、补偿、审计或多入口复用时使用：

    interfaces → application → domain ← infrastructure

- domain：模型、不变量、领域错误和端口，只依赖标准库。
- application：可选；编排用例，只依赖本模块 domain。
- infrastructure：实现数据库或外部服务端口，把技术错误翻译成领域错误。
- interfaces：入站接口层，包括 protobuf Service 和后台任务入口；完成协议转换并传递当前主体。

两个模块覆盖两种典型形态：`dictionary` 没有 application，展示纯 CRUD；`access` 有 application 且 domain 承载权限码、导航树和策略版本等真实不变量，也展示事务与用例编排。

单个仓储操作内部使用事务，本身不构成增加 application 的理由。已有真实编排的 application 可以保留同一用例下的简单查询转发。

跨模块用例由本模块 domain 声明所需能力，application 依赖该端口；本模块 infrastructure 适配到对方公开的 domain 端口或 application 用例。只有实际发生协作时才增加适配器。

不要为了目录对称引入工厂、领域事件或通用 DTO 体系。

## 数据所有权

全进程共用一个启动时选定的 PostgreSQL 或 MySQL database 和一个 Ent Client。表仍然归模块所有：跨模块读写对方的表要经过对方 domain 定义的端口，不在自己的 infrastructure 里直连别人的表。

这条约束在单体里没有编译器强制。`tests/architecture/data_ownership_test.go` 检查显式 SQL 表名、生成模型 import 和 Client 选择器；动态 SQL 与间接别名仍需 review。它的价值在拆分时才兑现——一张被三个模块直接查询的表，拆分时会同时变成三个模块的阻塞点。

需要依赖锁、唯一约束或数据库当前状态的不变量，在 infrastructure 的同一个事务内检查并写入；不要在 application 预检，那会引入 TOCTOU。

## 认证与授权

    Google/Apple ID Token → auth 验签并创建会话 → Eagle access token（ES256/RS256）
                                                  ↓ 本地公钥/JWKS 验签
    pkg/authn → pkg/identity.Principal
       ↓
    pkg/authz 中间件读 proto 的 access / perm
       ↓
    本地 Casbin 判定（策略在本库，5s 对账版本号）

- `auth` 模块通过官方 JWKS 验证 Google/Apple ID Token 的签名、issuer、audience、有效期与 nonce。
- Eagle access token 短期有效；refresh token 使用密码学随机值、数据库只保存 SHA-256 哈希，并在每次刷新时轮换。
- 权限要求声明在 proto 的 `access` / `perm` 上，handler 不写鉴权分支。可选级别只有 `PUBLIC` / `AUTHENTICATED` / `PERMISSION_REQUIRED`。
- Eagle token 使用带 `kid` 的非对称签名；认证服务独占私钥，通过 `/.well-known/jwks.json` 发布公钥。资源服务本地缓存公钥，不逐请求远程 introspection。
- token 只携带 `sub`、`sid`、`jti`、时间、audience 与角色，不携带邮箱、昵称等资料。角色按 audience 归属，角色能做什么一律由 Casbin 策略决定。
- 新账号在当前 audience 默认获得 `user`。管理员角色需在 `user_role_binding` 中显式分配，不从客户端请求或第三方资料推断。
- 手机号、微信等国内登录是未来可增加的独立验证用例；验证成功后必须复用 `user_account`、`user_identity`、会话和 Eagle token，不把短信验证码或 OAuth code 硬塞进 ID Token 接口。

### 统一认证中心的拆分路径

当多个应用需要共享账号、认证需要独立发布或区域合规要求出现时，先把 `auth` 及其四张自有表迁到独立
进程和数据库。网关继续把登录、刷新和 JWKS 路由到认证中心；业务服务保留 `pkg/authn`，通过
`RemoteKeySet` 缓存认证中心公钥并在本地构造 `Principal`，不逐请求同步调用认证中心。

`access` 可以继续留在业务单体；需要独立治理时再作为策略源拆出，通过版本化快照或事件更新各资源服务
的本地判定器。`ReplacePolicySnapshot` 保证切换原子性。服务间工作负载身份与 C 端用户会话是两类凭证，
不得复用用户 refresh token。国内与海外需要独立数据域时使用不同 issuer、密钥和账号存储，由网关按区域
路由；是否允许跨域绑定账号必须经过明确的合规与产品设计。

### 为什么单体还保留策略版本对账

`policy_sync.go` 的 reconciler 每 5 秒比对数据库里的策略版本号，变化时原子替换本地 Casbin 模型。这看起来像微服务遗留，但它解决的是**多副本**问题，不是多服务问题：单体水平扩容到 N 个副本后，在副本 A 上改的权限必须让副本 B 感知到。启动时还会校验 proto 里声明的权限码与数据库 catalog 一致，不一致直接 fail closed——半份策略比没有策略更危险。

## 部署拓扑

本地：

    Browser / App → Google / Apple ID Token → eagle:8000 → PostgreSQL / MySQL
                                                ↓
                                  Eagle JWT → authn → Casbin

`make up` 会先跑一次性迁移任务，成功后再启动应用。远端部署不启动数据库，只连接环境侧独立
管理的 PostgreSQL 或 MySQL；生产入口需要的 TLS 终止、请求限制和真实客户端 IP 由环境侧的 LB 或网关提供，
仓库不绑定具体网关。

生产是同一个镜像的两个 entrypoint：`/app/migrate` 跑迁移，`/app/eagle` 跑服务。schema 与代码同版本发布，不会出现「服务已升级、迁移还没跑」的窗口。服务进程启动时**不会**自动迁移；发布顺序固定为「迁移任务 → 服务滚动 → 观察」，编排方式（Compose、Kubernetes 或其它）由环境仓库自行维护。

## 共享代码边界

`pkg/` 只放无业务语义的技术原语：JWT 验签、授权中间件、健康检查、配置、进程生命周期和传输运行时。业务模型不能进 `pkg/`，`pkg/` 也不得 import `internal/`——一旦依赖方向反过来，技术设施开始依赖业务，两边就再也拆不开。

这些约束由 `tests/architecture/dependencies_test.go` 持续检查，包括分层依赖、模块边界、`pkg/` 方向、Ent Client 不出 infrastructure、禁止引入 DI 容器，以及一份明确拒绝的第三方库清单。

## 一致性与发布约束

权限正常每 5 秒对账，每次对账最多执行 5 秒。readiness 从首次探测到版本落后起容忍 30 秒，
持续落后即返回失败，追平后自动恢复。数据库读取失败立即使 readiness 失败。
这不是请求级即时撤权：部署环境必须持续探测并从流量池移除未就绪副本，探测和摘流也有延迟。
本地 Compose 仅标记容器健康状态，不自动停止向该容器发送请求。

退出登录立即撤销该 refresh token 的刷新能力，已签发 access token 仍可使用到到期（默认 15 分钟）。
账号停用或 audience 范围内的角色降级也在重新签发 token 后反映；修改角色的权限绑定则走策略对账。
会话创建/轮换与本地 access token 签发由一个原子端口完成：签发失败回滚事务，不消耗旧 refresh token。
事务提交后若网络响应丢失，客户端可能需要重新登录；当前不提供刷新响应重放或幂等恢复。

发布先运行新迁移，此时旧应用仍在运行；新应用启动失败也只回滚应用镜像。
所以每次迁移必须兼容仍在运行及允许回滚的应用版本，不能以同镜像包含迁移替代兼容性验证。
先扩展结构并保留旧字段，再迁移读写与历史数据，最后在旧版本退出回滚范围后收缩结构。
数据库 down 仅用于已验证的人工恢复，部署脚本不自动执行。

`tests/database` 从 goose 创建真实数据库，与 Ent 元数据比较表、字段类型/长度、空值约束、主键和必需索引。
SQL 额外定义的外键、CHECK、默认值和额外索引由迁移与仓储测试验证，不要求 Ent 表达完全相同的 DDL。
此检查不等于旧应用兼容性测试；涉及 schema 的发布还应按 `docs/migration-compatibility.md` 验证旧版本。
