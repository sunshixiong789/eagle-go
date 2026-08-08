# eagle-go

Go 微服务基础架子，对标 eagle cloud（Java 版）的系统底座：**认证 + 权限（RBAC）+ 字典**。

**Kratos v3** · **ent** · **Keycloak** · **Casbin** · PostgreSQL 17。

---

## 架构

```
                    ┌──────────────┐
   用户/客户端 ───→ │   Keycloak   │  用户 · 口令 · 角色的唯一来源
                    └──────┬───────┘
                           │ OIDC token（realm_access.roles）
                           ↓
                  ┌────────────────────┐
                  │   system 服务       │
                  │  ┌──────────────┐  │
                  │  │ authn 验签    │  │ ← 本地 JWKS，不回调 Keycloak
                  │  ├──────────────┤  │
                  │  │ authz 判定    │  │ ← Casbin，策略常驻内存
                  │  └──────────────┘  │
                  └─────────┬──────────┘
                            ↓
                 PostgreSQL 17  +  Redis
```

**职责切分**：Keycloak 回答「你是谁、你有哪些角色」，Casbin 回答「这个角色能不能调这个接口」。

之所以不把权限映射也放进 Keycloak：那样每次授权判定都要跨网络调用，且权限配置只能在 Keycloak 后台改。
之所以不把用户到角色的归属放进本库：那等于与 Keycloak 双写同一份数据，必然漂移。

## 技术选型

| 领域 | 选型 | 说明 |
|---|---|---|
| 服务框架 | [Kratos](https://github.com/go-kratos/kratos) v3.0.0 | 日志层已换成标准库 `log/slog` |
| API 契约 | Protobuf + [buf](https://buf.build) | `buf lint` / `buf breaking` 在 CI 拦截破坏性变更 |
| 参数校验 | [protovalidate](https://github.com/bufbuild/protovalidate) | 规则写在 proto 里，跨语言复用 |
| 数据访问 | [ent](https://entgo.io) + Raw SQL | Schema as Code，开了 `sql/execquery` 特性以便走原生 SQL |
| 数据库 | PostgreSQL 17 | 迁移用 [goose](https://github.com/pressly/goose)，不用 ent 自动迁移 |
| 认证 | **Keycloak** + OIDC | 用户、口令、角色的唯一来源 |
| 授权 | **[Casbin](https://casbin.org)** v2 | 角色→权限码映射，策略存本库 |
| 缓存 | Redis 7 + singleflight | 防击穿，故障时降级直连数据库 |
| 依赖注入 | [wire](https://github.com/google/wire) | 编译期生成，零反射 |
| 可观测 | OpenTelemetry | `contrib/otel/v3`，trace_id 自动注入日志 |

### 关于 Kratos v3

v3 是破坏性升级，网上的中文教程基本都是 v2 的，会误导。踩过的关键差异：

- **日志换成标准库 `*slog.Logger`**，v2 的 `log.Helper` 已不存在
- **OTel / JWT / protovalidate 全部移出核心**，分别在 `contrib/otel/v3`、`contrib/middleware/jwt/v3`、`contrib/middleware/validate/v3`
- aegis 依赖已内联进 `internal/ratelimit`，`middleware/ratelimit` 仍是 BBR 自适应限流
- **contrib 模块与 protoc 插件目前只有伪版本，没有 semver tag**，本项目统一 pin `v3.0.0-20260626125723-668db92c2c00`（官方 kratos-layout 自己也是 pin 伪版本）

---

## 权限模型：Spring Security `@PreAuthorize` 的等价物

Go 里没有 Spring Security 的对等物。本项目用 **proto 自定义注解 + 中间件 + Casbin** 复刻其声明式体验。

在契约上声明所需权限：

```protobuf
rpc CreatePermission(CreatePermissionRequest) returns (CreatePermissionResponse) {
  option (google.api.http) = { post: "/v1/system/permissions", body: "*" };
  option (eagle.annotations.v1.perm) = "system:permission:add";
}
```

`pkg/authz` 中间件在运行时从方法描述符读出该注解，交 Casbin 判定。判定顺序：

1. `public` 方法直接放行
2. 无主体 → **401**
3. 未声明权限码 → 已登录即可
4. 超管角色 → 短路放行
5. 其余 → Casbin 按 token 里的角色逐个判定

**失败方向是关闭的**：Casbin 判定出错、中间件未正确装配时一律拒绝，而不是放行。

### 两个通配相关的坑（都已写成回归测试）

**一、不要用 `keyMatch2`。** 它为 URL 路径设计，会把 `:xxx` 当成路径参数；
而权限码正是冒号分隔的，于是 `system:permission:query` 被解析成 `system:{任意}:{任意}`，
与 `system:permission:add` 匹配成功——**只读角色由此获得全部写权限**。

**二、通配只允许出现在末段。** Casbin 的 `keyMatch` 只看第一个 `*` 并做前缀匹配，
完全忽略其后的内容：`system:*:add` 在它眼里等价于 `system:*`，会连 `system:user:edit`
一起放行——写的人以为限定了动作，实际授出了整个域。
`domain.NewPermissionCode` 在构造阶段就拒绝这种形态。

合法形态只有两种：严格三段的具体权限码（`system:user:add`），
或末段为 `*` 的通配策略（`system:*`、`system:user:*`）。

### 领域层与 Casbin 的一致性由测试保证

`domain.PermissionCode.Covers` 和 Casbin 各有一份匹配实现——前者让「授权是否生效」
可以脱离 Casbin 单测、也是后台展示权限的依据，后者是运行时真正的守门人。
两者漂移轻则「后台显示已授权、调用却 403」，重则越权。

`TestDomainCoversMatchesCasbinEnforcement` 遍历策略×目标的组合逐条比对，
漂移发生时立刻变红，不靠注释约定同步。

---

## 快速开始

### 安装工具链

```bash
go install github.com/bufbuild/buf/cmd/buf@latest github.com/google/wire/cmd/wire@v0.7.0 github.com/pressly/goose/v3/cmd/goose@v3.27.3
```

### 代码生成

```bash
buf generate --template buf.gen.yaml && buf generate --template buf.gen.config.yaml && go generate ./ent && (cd app/system/cmd/server && wire)
```

### 运行测试（**不需要 Docker**）

```bash
go test ./...
```

集成测试用 [embedded-postgres](https://github.com/fergusstrange/embedded-postgres) 下载并在进程内拉起真实 PostgreSQL，
跑 `db/migrations` 下的真实迁移；Redis 侧用 miniredis。首次运行会下载 PG 二进制（约 100MB），之后从本地缓存启动。

端到端测试（`app/system/internal/e2e`）更进一步：真实 HTTP 服务器、
与生产一致的中间件链，外加一个签发**真实 RS256 JWT** 的 Keycloak 替身——
严格按 Keycloak 的 claim 结构下发，含 `realm_access.roles` 与
`resource_access.<client>.roles`——据此验证完整的 401 / 403 / 200 语义。

不用替身而直接构造 `Principal` 的话，最容易出错的一段恰好会被排除在测试之外：
签名验证、claim 名拼写、角色的嵌套层级。这几处写错都不会报错，
只会表现为「配了权限却还是 403」。

跳过集成测试：

```bash
go test -short ./...
```

### 竞态检测（Windows 上的坑）

CI 跑的是 `go test -race`，而 `-race` 依赖 cgo，需要 C 编译器。
**Windows 上没装 gcc 时它跑不起来**，报 `-race requires cgo`。

不想在本机装 MinGW 的话，用 Linux 容器跑（PostgreSQL 的 `initdb`
拒绝 root，所以要建一个普通用户）：

```bash
docker run --rm -v "$PWD:/src" -w /src golang:1.26 sh -c 'useradd -m -u 1500 t && su t -c "cd /src && HOME=/home/t GOPATH=/home/t/go GOCACHE=/home/t/c go test -race ./..."'
```

### 启动依赖与服务

```bash
docker compose -f deploy/docker-compose.yml up -d postgres redis keycloak
```

Keycloak 会自动导入 `deploy/keycloak/realm-eagle.json`（realm `eagle`，
角色 `admin` / `user`，三个客户端）。控制台 http://localhost:8080 ，初始账号 `admin/admin`。

**realm 里刻意不预置任何用户**——该文件会进版本库，内置已知口令的账号会一路带到生产。
首个管理员请在控制台创建并授予 realm 角色 `admin`。

执行迁移后启动：

```bash
goose -dir db/migrations postgres "postgres://eagle:eagle@127.0.0.1:5432/eagle?sslmode=disable" up
```

```bash
go run ./app/system/cmd/server -conf app/system/configs
```

要看链路和指标，再启可观测性栈（Grafana 已预置 Prometheus + Tempo 数据源）：

```bash
docker compose -f deploy/docker-compose.yml --profile obs up -d
```

然后把 `configs/config.yaml` 的 `otlp_endpoint` 填成 `127.0.0.1:4317`。

### 拿一个真实 token 试试

realm 里不预置用户，先创建一个（`Passw0rd!` 仅为示例）：

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh config credentials --server http://localhost:8080 --realm master --user admin --password admin
```

创建用户时**必须带 firstName/lastName**，否则 Keycloak 26 的 `VERIFY_PROFILE`
必需动作会让登录报 `Account is not fully set up`：

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh create users -r eagle -s username=alice -s enabled=true -s firstName=Alice -s lastName=Test -s email=alice@example.com
```

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh set-password -r eagle --username alice --new-password 'Passw0rd!'
```

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh add-roles -r eagle --uusername alice --rolename admin
```

取 token 并调接口：

```bash
curl -s -d client_id=eagle-web -d username=alice -d 'password=Passw0rd!' -d grant_type=password http://127.0.0.1:8080/realms/eagle/protocol/openid-connect/token
```

```bash
curl -s -H "Authorization: Bearer <access_token>" http://127.0.0.1:8000/v1/system/permissions
```

服务间调用用服务账号（`client_credentials`）：

```bash
curl -s -d client_id=eagle-worker -d client_secret=dev-only-worker-secret -d grant_type=client_credentials http://127.0.0.1:8080/realms/eagle/protocol/openid-connect/token
```

### 配置里的时长写法

所有 duration 由 `google.protobuf.Duration` 承载，**只接受「秒数 + s」**：`3600s`、`0.5s`。
Go 风格的 `1h` / `30m` / `500ms` 会解析失败并让服务在启动阶段直接崩溃。

这个坑真实踩过——配置里写了 `30m`，服务起不来，而报错信息只说
`invalid google.protobuf.Duration value`，不指向具体字段。
`conf_test.go` 现在会真实加载配置文件，把这类错误挡在 CI。

---

## 分层：DDD Lite + Clean Architecture + Kratos

依赖方向自外向内单向流动，`domain` 处在最内层且不依赖任何东西：

```
server ──→ service ──→ biz(用例) ──→ domain
                          data ────────┘   （实现 domain 定义的仓储接口）
```

| 层 | 职责 | 允许依赖 |
|---|---|---|
| `server` | HTTP/gRPC 装配、中间件链 | service, conf |
| `service` | proto ↔ 领域对象互转，从 context 取调用者身份 | biz, domain |
| `biz` | 用例编排 + 领域错误到状态码的映射 | domain |
| **`domain`** | **实体（含不变量）· 值对象 · 仓储接口** | **无** |
| `data` | 仓储实现：ent + Redis + Casbin 适配 | domain |

### 战术 DDD 只用在有不变量的地方

这是有意的取舍。授权域（权限树、角色绑定）有真实的不变量要守护，用聚合根和值对象；
字典是纯 CRUD，套聚合根只增加仪式感而无收益，就保持贫血。

**值对象 `PermissionCode`**：权限码同时出现在 proto 注解、Casbin 策略、权限树三处，
任何一处走样都会造成「配置看起来成功了但永远不生效」的静默失败。
提升为值对象后，格式规则只有一份，越界的值构造不出来。

**聚合根 `Permission`**：字段全部私有，只能经 `NewPermission` / `Update` 修改，
不变量（名称非空、类型合法、按钮必须有权限码）在方法里就地校验——
不存在「构造出一个违反不变量的实体」的路径。

**集合视图 `PermissionTree`**：单个聚合根看不到兄弟和祖先，
所以「防环」「补全祖先链」这类跨节点规则放在树上，而不是硬塞进 Permission。

回报是领域规则可以完全脱离数据库单测：

```bash
go test ./app/system/internal/domain/...
```

0.4 秒跑完，不启动任何外部依赖。

## 目录结构

```
eagle-go/
├── api/eagle/
│   ├── annotations/v1/     # 自定义权限注解 (perm / public)
│   └── system/v1/          # 权限树 · 字典 · 角色权限绑定
├── app/system/
│   ├── cmd/server/         # 入口 + wire 装配
│   └── internal/
│       ├── server/         # HTTP/gRPC 装配、中间件链
│       ├── service/        # proto ↔ 领域对象转换
│       ├── biz/            # 用例编排 + 错误映射
│       ├── domain/         # 实体 · 值对象 · 仓储接口（零依赖）
│       ├── data/           # 仓储实现：ent + Redis + Casbin
│       └── conf/           # 配置契约
├── ent/schema/             # ent Schema as Code
├── pkg/
│   ├── authn/              # Keycloak JWKS 验签 + Redis 撤销黑名单
│   ├── authz/              # Casbin 判定器 + 适配器 + 中间件
│   ├── identity/           # context 中的已认证主体
│   ├── db/                 # ent 客户端 + 事务封装
│   └── redisx/             # 缓存原语（singleflight 防击穿）
├── db/migrations/          # goose 迁移
└── deploy/
```

---

## 服务间认证

当前用 **Keycloak client_credentials**（服务账号），复用同一套 JWKS 验签链路，零新增基础设施。
服务账号与终端用户走同一套 Casbin 判定——两者的角色都由 Keycloak 下发，不为服务单开一套授权语义。

**何时该切到 SPIFFE/SPIRE**：服务数到几十个使得 client secret 的分发轮转成为真实负担、
跨集群统一工作负载身份、合规要求传输层 mTLS，或已决定上 Istio（直接用网格自带的 SPIFFE 身份）。

注意 SPIFFE 解决的是「调用方是哪个工作负载」，不解决「代表哪个用户」——用户上下文仍需 token 传递，两者互补。
切换点收敛在 `pkg/authn` 的身份提取层，`pkg/authz` 与业务代码零改动。

---

## 当前状态

| 模块 | 状态 |
|---|---|
| 工具链与代码生成（buf / ent / wire） | ✅ 已验证 |
| 数据库迁移与种子数据 | ✅ 真实 PG 上 up→down→up 往返验证 |
| 权限树 · 字典 · 角色权限绑定 | ✅ 数据层已验证 |
| Casbin 鉴权中间件 | ✅ 已验证（401/403/200、通配边界、角色继承、失败关闭） |
| 可观测性（OTel 链路 + Prometheus 指标） | ✅ 已装配，配置解析有回归测试 |
| 部署（Dockerfile · compose · Keycloak realm） | ✅ 已就绪 |
| 端到端鉴权链路 | ✅ 真实 HTTP + 真实签名 JWT 验证 401/403/200 语义 |
| 对接真实 Keycloak | ✅ 真实 realm 导入、真实 token 签发、完整鉴权链路已跑通 |
| Helm chart · 生产部署 | 🚧 待完成 |

## 安全提示

- `configs/config.yaml` 只保留结构和安全默认值，敏感项经环境变量或 K8s Secret 注入
- 迁移中不创建任何账号——内置已知口令的账号会一路带到生产。用户全部在 Keycloak 侧管理
