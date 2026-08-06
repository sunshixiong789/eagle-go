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

### 通配用 `keyMatch` 而非 `keyMatch2`

这是一个曾经踩过的坑，已写成回归测试。`keyMatch2` 为 URL 路径设计，会把 `:xxx` 当成路径参数；
而权限码正是冒号分隔的，于是 `system:permission:query` 被解析成 `system:{任意}:{任意}`，
与 `system:permission:add` 匹配成功——**只读角色由此获得全部写权限**。

`keyMatch` 只认 `*`：`system:*` 覆盖整个 system 域，不含 `*` 的策略退化为精确比较。

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

跳过集成测试：

```bash
go test -short ./...
```

### 执行迁移

```bash
goose -dir db/migrations postgres "postgres://eagle:eagle@127.0.0.1:5432/eagle?sslmode=disable" up
```

---

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
│       ├── service/        # proto ↔ 领域模型转换
│       ├── biz/            # 用例编排 + 仓储接口（不依赖任何基础设施）
│       ├── data/           # 仓储实现：ent + Redis + Casbin 适配
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

分层遵循依赖倒置：**`biz` 定义仓储接口，`data` 实现它**。
`biz` 不 import 任何 ent/redis/casbin/protobuf，领域规则可以脱离基础设施单测。

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
| Keycloak 接入 | 🚧 验签链路已就绪，realm 配置与端到端联调待完成 |
| 可观测性与交付 | 🚧 待完成 |

## 安全提示

- `configs/config.yaml` 只保留结构和安全默认值，敏感项经环境变量或 K8s Secret 注入
- 迁移中不创建任何账号——内置已知口令的账号会一路带到生产。用户全部在 Keycloak 侧管理
