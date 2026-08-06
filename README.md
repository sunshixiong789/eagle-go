# eagle-go

Go 微服务基础架子，对标 eagle cloud（Java 版）的系统底座：**认证中心 + 权限（RBAC）+ 字典**。

基于 **Kratos v3**、PostgreSQL、sqlc、OAuth2/OIDC 构建。

---

## 技术选型

| 领域 | 选型 | 说明 |
|---|---|---|
| 服务框架 | [Kratos](https://github.com/go-kratos/kratos) v3.0.0 | 日志层已换成标准库 `log/slog` |
| API 契约 | Protobuf + [buf](https://buf.build) | `buf lint` / `buf breaking` 在 CI 拦截破坏性变更 |
| 参数校验 | [protovalidate](https://github.com/bufbuild/protovalidate) | 规则写在 proto 里，跨语言复用 |
| 数据访问 | [sqlc](https://sqlc.dev) + pgx/v5 | 手写 SQL 生成类型安全代码 |
| 数据库 | PostgreSQL 17 | 迁移用 [goose](https://github.com/pressly/goose) |
| 缓存 | Redis 7 + singleflight | 防击穿，故障时降级直连数据库 |
| 依赖注入 | [wire](https://github.com/google/wire) | 编译期生成，零反射 |
| 认证 | [zitadel/oidc](https://github.com/zitadel/oidc) | OAuth2/OIDC 授权服务器（开发中） |
| 口令哈希 | argon2id | PHC 格式，参数随串存储 |
| 可观测 | OpenTelemetry | `contrib/otel/v3`，trace_id 自动注入日志 |

### 关于 Kratos v3

v3 是破坏性升级，网上的中文教程基本都是 v2 的，会误导。本项目踩过的关键差异：

- **日志换成标准库 `*slog.Logger`**，v2 的 `log.Helper` 已不存在
- **OTel / JWT / protovalidate 全部移出核心**，分别在 `contrib/otel/v3`、`contrib/middleware/jwt/v3`、`contrib/middleware/validate/v3`
- aegis 依赖已内联进 `internal/ratelimit`，`middleware/ratelimit` 仍是 BBR 自适应限流
- **contrib 模块与 protoc 插件目前只有伪版本，没有 semver tag**，本项目统一 pin `v3.0.0-20260626125723-668db92c2c00`（官方 kratos-layout 自己也是 pin 伪版本）

---

## 权限模型：Spring Security `@PreAuthorize` 的等价物

Go 里没有 Spring Security 的对等物。本项目用 **proto 自定义注解 + 中间件**复刻其声明式体验。

在契约上声明所需权限：

```protobuf
rpc CreateUser(CreateUserRequest) returns (CreateUserResponse) {
  option (google.api.http) = { post: "/v1/system/users", body: "*" };
  option (eagle.annotations.v1.perm) = "system:user:add";
}
```

`pkg/authz` 中间件在运行时从方法描述符读出该注解并判定，业务 handler 里不出现任何鉴权代码。判定顺序：

1. `public` 方法直接放行
2. 无主体 → **401**
3. 未声明权限码 → 已登录即可
4. 服务令牌（client_credentials）→ 按 token scope 判定
5. 超管角色 → 短路放行，不查权限表
6. 其余 → 查用户权限码集合（Redis 缓存）

**失败方向是关闭的**：权限加载出错、中间件未正确装配时一律拒绝，而不是放行。

> 功能权限第一版**不用 Casbin**——本质是「权限码集合里有没有」，一次 set 查询就够。
> Casbin 留给数据权限（本部门及以下 / 仅本人）这类真正的 ABAC 场景，`pkg/authz` 已留扩展点。

---

## 快速开始

### 安装工具链

```bash
go install github.com/bufbuild/buf/cmd/buf@latest
go install github.com/google/wire/cmd/wire@v0.7.0
go install github.com/pressly/goose/v3/cmd/goose@v3.27.3
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
```

### 代码生成

```bash
buf generate --template buf.gen.yaml
```

```bash
buf generate --template buf.gen.config.yaml
```

```bash
cd db && sqlc generate
```

```bash
cd app/system/cmd/server && wire
```

### 运行测试（**不需要 Docker**）

```bash
go test ./...
```

集成测试用 [embedded-postgres](https://github.com/fergusstrange/embedded-postgres) 下载并在进程内拉起真实 PostgreSQL，用 miniredis 提供进程内 Redis。首次运行会下载 PG 二进制（约 100MB），之后从本地缓存启动。

跳过集成测试：

```bash
go test -short ./...
```

### 启动服务

需要 PostgreSQL 和 Redis。仓库提供了 compose 文件：

```bash
docker compose -f deploy/docker-compose.yml up -d postgres redis
```

执行迁移后启动：

```bash
goose -dir db/migrations postgres "postgres://eagle:eagle@127.0.0.1:5432/eagle?sslmode=disable" up
```

```bash
go run ./app/system/cmd/server -conf app/system/configs
```

---

## 目录结构

```
eagle-go/
├── api/eagle/
│   ├── annotations/v1/     # 自定义权限注解 (perm / public)
│   └── system/v1/          # 用户·角色·权限·字典契约 + 内部接口
├── app/
│   ├── system/             # 系统服务
│   │   ├── cmd/server/     # 入口 + wire 装配
│   │   └── internal/
│   │       ├── server/     # HTTP/gRPC 装配、中间件链
│   │       ├── service/    # proto ↔ 领域模型转换
│   │       ├── biz/        # 用例编排 + 仓储接口（不依赖任何基础设施）
│   │       ├── data/       # 仓储实现：sqlc + Redis
│   │       └── conf/       # 配置契约
│   └── auth/               # 认证中心（OIDC OP，开发中）
├── pkg/
│   ├── authn/              # JWKS 验签 + Redis 撤销黑名单
│   ├── authz/              # 权限中间件（读 proto 注解）
│   ├── identity/           # context 中的已认证主体
│   ├── db/                 # pgx 连接池 + 事务封装 + sqlc 生成物
│   ├── password/           # argon2id
│   └── redisx/             # 缓存原语（singleflight 防击穿）
├── db/
│   ├── migrations/         # goose 迁移
│   ├── query/              # sqlc 输入 SQL
│   └── sqlc.yaml
└── deploy/
```

分层遵循依赖倒置：**`biz` 定义仓储接口，`data` 实现它**。`biz` 不 import 任何 pgx/redis/protobuf，领域规则可以脱离基础设施单测。

---

## 当前状态

| 模块 | 状态 |
|---|---|
| 工具链与代码生成 | ✅ 已验证 |
| 数据库迁移与种子数据 | ✅ 已在真实 PG 上验证 |
| system 服务（用户·角色·权限·字典） | ✅ 数据层已验证，HTTP/gRPC 待联调 |
| 鉴权中间件 | ✅ 已验证（401/403/200、超管短路、服务令牌、失败关闭） |
| 认证中心（OIDC OP） | 🚧 开发中 |
| 可观测性与交付 | 🚧 待完成 |

管理员账号刻意不在迁移里创建——把已知口令的账号写进版本库会一路带到生产。

## 安全提示

- `signing_key.private_key_pem` 目前明文落库，生产环境应改为 KMS/Vault 托管或信封加密
- `configs/config.yaml` 只保留结构和安全默认值，敏感项经环境变量或 K8s Secret 注入
