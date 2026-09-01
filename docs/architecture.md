# Eagle 架构

本仓库是**单进程单体**：一个 Go module、一个二进制、一个数据库、一个镜像。分层与模块边界依然显式存在，但它们由目录约定和架构测试保证，不再由进程和网络边界保证。

这是一个刻意的取舍。进程拆分带来的独立发布、独立扩缩容和故障隔离，代价是分布式事务、跨进程调用的失败模式、多份配置与多套部署清单。除非确实存在这些收益的需求，单体在同等业务复杂度下的正确性成本要低得多。保留清晰的模块边界，是为了将来真的出现拆分信号时，迁出的是一个已经内聚的目录，而不是一团缠绕的调用。

## 模块

| 模块 | 职责 | 拥有的数据 | 外部依赖 |
|---|---|---|---|
| `access` | 权限码目录、导航树、角色绑定、Casbin 策略 | `permission_definition`、`casbin_rule`、策略版本 | 无 |
| `dictionary` | 字典的简单 CRUD，作为新模块的样板 | `dict_type`、`dict_data` | 无 |
| `file` | 上传编排、元数据、生命周期清理 | `file` 元数据表 | 对象存储（本地目录或 S3） |

IdP（默认 Keycloak）是独立的认证中心，负责用户、口令、角色和 token。**本仓库不建用户表**。

## 目录表达什么

    api/eagle/<module>/v1               API 契约（proto，只生成 HTTP）
    cmd/eagle                           进程入口和唯一组合根（Wire）
    internal/<module>                   业务模块
    internal/platform/database/ent      Ent Client 与 schema
    migrations                          goose SQL 迁移
    pkg                                 无业务语义的共享技术模块
    tools                               生成器与迁移程序（独立 go.mod）
    tests                               架构测试、e2e 与测试工具
    deploy                              本地与生产部署资源

`internal/` 是 Go 的编译器可见性边界，仓库外无法 import。仓库内的模块边界由 `tests/architecture` 检查：模块之间不得 import 对方的 `service` 或 `infrastructure`。

`tools/` 保留独立 `go.mod`，是为了让 buf、goose、golangci-lint 和 protoc 插件的版本被锁定，又不进入业务依赖图。生成的二进制落在 `bin/`，由 Makefile 在仓库根目录调用——工具进程的工作目录必须是仓库根，否则 `buf.gen.yaml` 的相对路径、goose 的 `-dir` 和 wire 的包路径都会失效。

## 模块内渐进式分层

模块不按目录数量评价 DDD，而是按用例复杂度选择最小结构。纯 CRUD 或查询使用：

    service → domain ← infrastructure

存在多端口协作、聚合加载-变更-保存、事务、幂等、补偿、审计或多入口复用时使用：

    service → application → domain ← infrastructure

- domain：模型、不变量、领域错误和端口，只依赖标准库。
- application：可选；编排用例，只依赖本模块 domain。
- infrastructure：实现数据库、对象存储等端口，把技术错误翻译成领域错误。
- service：入站适配，包括 protobuf Service 和后台任务入口；完成协议转换并传递当前主体。

三个模块正好覆盖三种形态：`dictionary` 没有 application（纯 CRUD，加一层只会得到 `return repo.Xxx(...)`）；`file` 有 application（上传需要「写元数据 → 落对象存储 → 失败回滚」的编排，且上传接口与后台清理任务复用同一个 `Usecase`）；`access` 有 application 且 domain 承载真实不变量（权限码的三段格式、导航树的父子完整性，以及策略变更的版本乐观并发）。

不要为了目录对称引入工厂、领域事件或通用 DTO 体系。

## 数据所有权

全进程共用一个 PostgreSQL database 和一个 Ent Client。表仍然归模块所有：跨模块读写对方的表要经过对方 domain 定义的端口，不在自己的 infrastructure 里直连别人的表。

这条约束在单体里没有编译器强制，只能靠 review 和架构测试的模块边界规则。它的价值在拆分时才兑现——一张被三个模块直接查询的表，拆分时会同时变成三个模块的阻塞点。

需要依赖锁、唯一约束或数据库当前状态的不变量，在 infrastructure 的同一个事务内检查并写入；不要在 application 预检，那会引入 TOCTOU。

## 认证与授权

    用户 token
       ↓ 本地验签（JWKS，无 OIDC discovery）
    pkg/authn → pkg/identity.Principal
       ↓
    pkg/authz 中间件读 proto 的 access / perm
       ↓
    本地 Casbin 判定（策略在本库，5s 对账版本号）

- 本服务是纯粹的 **OIDC 资源服务器**：只用 JWKS 验签 + 读 claim，不建用户表，不做 OIDC discovery，不依赖任何 IdP 专有接口。
- 权限要求声明在 proto 的 `access` / `perm` 上，handler 不写鉴权分支。可选级别只有 `PUBLIC` / `AUTHENTICATED` / `PERMISSION_REQUIRED`。
- 角色分两级命名空间：realm 角色 `admin` → `realm:admin`，本 client 的角色 `admin` → `client:eagle-api:admin`。`auth.super_admin_role` 的短路**只认 client 角色**，否则任何 realm 级的 `admin` 都会顺带拿到本服务全部权限。
- 换 IdP（Logto / Auth0 / Authing）**不改代码**：JWKS 路径和 role claim 路径都在 `auth` 配置里，对照表见 [`deploy/keycloak/README.md`](../deploy/keycloak/README.md)。苹果、Google 等第三方登录在 IdP 侧配置，应用只管拿到的那张 token。

### 为什么单体还保留策略版本对账

`policy_sync.go` 的 reconciler 每 5 秒比对数据库里的策略版本号，变化时原子替换本地 Casbin 模型。这看起来像微服务遗留，但它解决的是**多副本**问题，不是多服务问题：单体水平扩容到 N 个副本后，在副本 A 上改的权限必须让副本 B 感知到。启动时还会校验 proto 里声明的权限码与数据库 catalog 一致，不一致直接 fail closed——半份策略比没有策略更危险。

## 部署拓扑

本地：

    Browser → nginx 网关:8000 → eagle:8000
                                   │
                        PostgreSQL ─┼─ MinIO
                                    └─ Keycloak

`make up` 会先跑一次性迁移任务，成功后再启动应用和网关。OTel、Prometheus、Alertmanager、Tempo、Loki、Grafana 通过 `--profile obs` 按需启动。

网关只有一个上游，看起来是多余的。保留它是因为生产入口该有的东西（TLS 终止、请求体上限、真实客户端 IP）都在这一层，本地就带上可以避免「本地能传 50MB、生产被网关拦掉」这类只在上线时才暴露的差异。

生产是同一个镜像的两个 entrypoint：`/app/migrate` 跑迁移，`/app/eagle` 跑服务。schema 与代码同版本发布，不会出现「服务已升级、迁移还没跑」的窗口。具体发布顺序见[生产环境部署](deployment.md)。

## 共享代码边界

`pkg/` 只放无业务语义的技术原语：JWT 验签、授权中间件、健康检查、配置、进程生命周期和传输运行时。业务模型不能进 `pkg/`，`pkg/` 也不得 import `internal/`——一旦依赖方向反过来，技术设施开始依赖业务，两边就再也拆不开。

这些约束由 `tests/architecture/dependencies_test.go` 持续检查，包括分层依赖、模块边界、`pkg/` 方向、Ent Client 不出 infrastructure、Wire 只在组合根，以及一份明确拒绝的第三方库清单。
