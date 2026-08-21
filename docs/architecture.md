# Eagle 架构边界

本仓库是模块化单体：当前只有一个 `server` 进程，业务代码先按模块隔离；只有独立扩缩容、故障域或发布周期成立时，才把模块迁成独立服务。

## 顶层目录表达什么

- `cmd/server` 是唯一可执行程序和组合根。
- `internal` 是 Go 的编译器可见性边界，表示仓库私有实现，不表示“内部服务”。
- `internal/modules` 放业务能力；每个一级目录是一个业务模块。
- `internal/platform` 放配置、数据库连接和网络服务器等进程级能力，不是业务模块。
- `tests` 放跨模块架构测试、端到端测试和测试工具。
- `api/eagle/<module>/v1` 按业务模块保存对外契约，HTTP 路径可独立保持兼容。

## 模块优先，模块内四层

每个业务模块的依赖方向固定为：

`interfaces → application → domain ← infrastructure`

- `domain`：模型、不变量、领域错误和端口，只依赖标准库。
- `application`：用例编排，只依赖 domain。
- `infrastructure`：Ent、事务、Casbin、文件系统等端口实现。
- `interfaces`：protobuf 与领域对象转换，将当前主体交给用例。
- `internal/platform/server`：HTTP/gRPC 注册、中间件链和统一错误映射。
- `internal/platform/database`：共享数据库连接生命周期，不包含业务仓储。

这些边界由 `tests/architecture/dependencies_test.go` 检查。

当前模块：

| 模块 | 边界 |
|---|---|
| `access` | 权限目录、导航树、角色权限与继承 |
| `dictionary` | 字典类型和字典项 |
| `file` | 小文件元数据、所有者隔离、BlobStore 端口 |
| `notification` | 站内通知、未读计数和已读状态 |

`access` 有真实不变量，使用聚合根和值对象；字典和通知保持简单实体。DDD 四层是依赖边界，不是要求每个模块都创建领域事件、工厂和领域服务。

## 身份与授权

- Keycloak 负责登录、用户身份、角色归属和 token 签发，本仓库不建用户表、不存口令。
- `pkg/authn` 本地验证 OIDC/JWT 并把主体放入 context。
- `access` 保存角色到权限的映射以及角色继承。
- 每个 RPC 在 proto 上声明 `access`；需要权限时同时声明严格三段 `perm`。
- `permission_definition` 是后端授权契约目录；`navigation_node` 是前端导航，二者不能互相代替。
- 策略更新使用数据库 version、审计和周期对账，不引入 Redis、MQ 或 outbox。

## 文件与通知的当前边界

文件模块默认使用本地磁盘适配器，适合开发和单实例部署：

- HTTP 上传/下载使用原始二进制 body，不做 JSON base64。
- 元数据存 PostgreSQL，BlobStore 端口隔离具体存储。
- 读取和删除按 token `sub` 做所有者隔离。
- 单文件大小由 `file.max_size_bytes` 限制。

生产需要多副本时，实现同一个 BlobStore 端口接 S3/OSS/MinIO，再切换装配；不要让业务层依赖厂商 SDK。

通知模块当前只做站内通知。短信、邮件、Push 只有在真实渠道、重试和吞吐需求出现后再扩展，届时通常值得独立部署；当前不预埋 MQ、模板中心和通道表。

## 何时拆成服务

代码模块不等于部署服务。满足至少一项再拆：

- 需要独立扩缩容或资源模型明显不同；
- 需要独立故障隔离；
- 有独立团队和发布周期；
- 被多个系统跨边界复用；
- 文件流量或通知异步吞吐已影响当前 server 进程。

拆分后为新进程建立独立入口和仓库边界，通过 API 或事件交互，禁止跨服务 import 对方内部实现或直接读写对方拥有的表。

## `pkg` 与业务模块

`pkg` 只放无业务语义且会被多个服务复用的技术原语，例如验签、鉴权中间件、数据库连接、探活和追踪。菜单、文件元数据、通知、字典等业务概念必须留在模块内。
