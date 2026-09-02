# 单体开发脚手架瘦身设计

## 目标

将仓库从包含多个业务样例和生产运维设施的完整应用，收敛为可直接复制使用的 Go 单体开发脚手架。脚手架保留通用安全、数据、可观测性和本地开发能力，同时只保留一个最小 CRUD 示例，避免使用者先删除与自身业务无关的代码。

本仓库不承担既有生产数据库升级，因此允许重建迁移基线。

## 保留范围

- 单进程、单 Go module、单 PostgreSQL 数据库和单镜像。
- Kratos HTTP、Protobuf/buf、Wire、Ent 和 goose 工具链。
- `access` 模块：OIDC 身份认证、Casbin RBAC、权限目录和角色权限绑定。
- `dictionary` 模块：唯一的简单 CRUD 示例，采用 `service -> domain <- infrastructure`。
- `pkg/authn`、`pkg/authz`、`pkg/identity`、数据库、配置、HTTP 服务、健康检查及可观测性等通用技术能力。
- 结构化日志、健康检查、Prometheus 指标和可选 OTLP trace 埋点。
- Dockerfile，以及用于本地开发的 PostgreSQL、Keycloak、迁移和应用 Compose 服务。
- 架构、单元、集成和端到端测试。

## 删除范围

- `file` 模块及其 Proto、领域、应用、服务、基础设施、测试和错误映射。
- 文件上传过滤器、文件配置、清理 worker、S3/本地对象存储适配器。
- MinIO Compose 服务、初始化任务、数据卷和 MinIO Go SDK。
- 与文件相关的 Ent Schema、迁移和权限种子。
- 历史迁移链，改为只表达当前脚手架结构的单一基线迁移。
- 发布 workflow、网关、Kubernetes/多节点部署和生产运维说明。
- Grafana、Loki、Tempo、Prometheus Server、Alertmanager、Alloy、OTel Collector 等绑定具体平台的部署组件。
- README、配置、测试和生成产物中对上述能力的引用。

## 最终结构

```text
api/eagle/
├── access/v1/
├── annotations/v1/
└── dictionary/v1/

internal/
├── access/
│   ├── application/
│   ├── domain/
│   ├── infrastructure/
│   └── service/
├── dictionary/
│   ├── domain/
│   ├── infrastructure/
│   └── service/
└── platform/database/

deploy/
├── docker-compose.yml
├── keycloak/
└── postgres/

migrations/
└── 00001_baseline.sql
```

## 架构和运行链路

进程启动时依次加载配置和可观测性设施、连接 PostgreSQL、初始化 OIDC 验签器与 Casbin、装配 `access` 和 `dictionary`，最后启动业务 HTTP 服务及独立健康/指标端点。

`dictionary` 保持直接 CRUD，不增加只转发调用的 application 层。`access` 保留 application 层，用于承载权限树、角色绑定、策略一致性和事务编排，继续作为有业务不变量模块的参考实现。

认证和鉴权由 HTTP 中间件统一完成。每个 RPC 在 Proto 中显式声明访问级别和权限码，handler 只负责协议转换、主体传递和领域错误边界适配。

## 数据设计

删除现有增量迁移历史，新建 `00001_baseline.sql`。基线只包含：

- 权限定义、权限树状态及权限相关审计/策略状态表；
- Casbin 策略表；
- 字典类型和字典数据表；
- 脚手架运行所需的权限定义和导航种子。

Ent Schema 必须与基线迁移一致。删除文件 Schema 后重新生成 Ent 代码，不手工编辑生成目录。

## 配置与本地开发

配置根仅保留 server、data、auth 和 observability。删除 file 配置及对应环境变量。

Compose 仅保留 PostgreSQL、Keycloak、一次性 migration 和 app。`make up-deps` 启动 PostgreSQL 与 Keycloak；宿主机可继续通过 `make migrate-up` 和 `make run` 调试。Keycloak realm 保留脚手架需要的 client、角色、audience mapper 和权限示例，不保留文件权限。

## 错误处理

保留统一的认证失败、授权失败、协议校验和领域错误映射。`access` 与 `dictionary` 的领域错误继续映射为稳定的 HTTP/Proto 错误原因。删除所有文件领域错误和上传体积限制处理。

基础设施层继续把数据库唯一约束、未找到和并发冲突翻译为领域错误，service 不接触 Ent 或事务对象。

## 生成与依赖

只修改 Proto、配置 Proto、Ent Schema、Wire 声明等源文件，然后运行 `make generate` 重建 API、配置、Ent、Wire 和 OpenAPI 等生成结果。运行 `go mod tidy` 后，MinIO SDK及仅由它引入的间接依赖应从业务 module 消失。

## 测试与验收

- `access` 的领域不变量、应用编排、仓储事务和授权测试继续通过。
- `dictionary` 的领域、仓储、service 和 HTTP 行为测试继续通过。
- 架构测试仍能发现至少一个业务模块，并校验既有依赖规则。
- 配置加载、数据库迁移、OIDC/RBAC 边界、健康检查和端到端测试继续通过。
- `make generate` 成功，且再次生成不产生新的差异。
- 修改过的 Go 文件经过格式化；运行相关测试，并在环境允许时运行 `make lint` 和 `make test`。
- 全仓检索不再出现 `file` 业务、S3、MinIO 或已删除运维组件的有效引用。

## 非目标

- 不引入新的业务模块、通用 Repository、DTO/DO、mapper 或为未来需求预留的抽象。
- 不将单体拆为微服务，也不加入消息队列、缓存、服务发现或内部 RPC。
- 不提供生产集群、网关或具体可观测平台的部署方案。
- 不兼容旧迁移版本或已有数据库数据。
