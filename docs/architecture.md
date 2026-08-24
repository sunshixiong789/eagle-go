# Eagle 微服务架构

本仓库是 Go Workspace 组织的多模块微服务单仓库，不是模块化单体。每个 `app/<service>` 都有独立 `go.mod`、Ent Client、迁移、配置和进程入口；共享源码只限技术运行时和明确的 API 契约。

## 服务边界

| 进程 | 拥有的模块 | 拥有的数据 | 依赖 |
|---|---|---|---|
| admin | access、dictionary、file、notification | 权限、字典、文件元数据、站内通知、事件 Inbox | Keycloak、S3、RabbitMQ |
| product | product | 商品 | admin 授权判定、Redis 缓存 |
| order | order | 订单、商品快照、事件 Outbox | product 查询、RabbitMQ |

Keycloak 是独立认证中心，负责用户、口令、角色和 token。本仓库不建用户表。

admin 不是业务流量网关。它组合四个基础模块，是因为这些能力当前都属于低流量管理/支撑面，没有独立扩缩容、团队或故障隔离需求。模块边界仍然保留；将来出现真实拆分信号时可以迁出，而不是现在就为每个模块维护一套镜像和数据库。

## 目录表达什么

    api/eagle/<module>/v1                         跨进程契约（独立 Go module）
    app/<service>/cmd/<service>                   进程入口和唯一组合根（Wire）
    app/<service>/internal/<module>               服务拥有的业务模块
    app/<service>/internal/platform/database/ent  服务独占的 Ent Client
    app/<service>/migrations                      服务独占的数据库迁移
    app/<service>/configs                         服务配置
    pkg                                           无业务语义的共享技术模块
    tools                                         生成器和迁移程序模块
    tests                                         架构测试与跨模块测试工具
    deploy                                        本地与生产部署资源

`app/<service>/internal` 是 Go 的编译器可见性边界：其他服务连实现包都无法 import。服务边界同时由独立 module、入口、Ent Client、数据库、API 调用和架构测试保证。根 `go.work` 只组合本地开发工作区，每个模块的依赖仍由自己的 `go.mod/go.sum` 管理。进程对象图由该服务 `cmd/<service>` 的 Wire injector 生成；`google/wire` 不能进入业务四层。

## 模块内四层

每个模块固定使用：

    service → application → domain ← infrastructure

- domain：模型、不变量、领域错误和端口，只依赖标准库。
- application：编排用例，只依赖本模块 domain。
- infrastructure：实现数据库、文件存储和跨服务客户端端口。
- service：实现 protobuf Service，完成 protobuf 与 domain 转换并传递当前主体。

DDD 是依赖边界，不是代码数量指标。商品是简单 CRUD；订单有“商品不可用、重复商品、金额快照和总额”这些真实不变量，才使用聚合根。不要为了四层引入工厂、领域事件或通用 DTO 体系。

## 数据所有权

本地 Docker Compose 只复用一个 PostgreSQL 实例以节省资源，但创建三个独立 database。生产可以复用数据库集群，仍必须做到：

- 每个服务使用独立 database 和账号；
- 迁移只执行 `app/<service>/migrations`；
- 禁止跨服务查询、外键和事务；
- 其他服务的数据只能通过 API 或已定义事件读取。

订单不会读取商品表，也不会把客户端报价当真。创建订单时通过 product gRPC 批量读取商品，将 SKU、名称和单价保存为订单快照，再只在订单库内提交一次事务。商品以后改名或改价不会篡改历史订单。

订单创建事务会同时写入 `event_outbox`，后台 relay 经 RabbitMQ publisher confirm
发布 `OrderCreatedV1`。admin 的通知消费者在同一事务内先写 `event_inbox`，再写
通知，因此重复投递不会产生重复通知。RabbitMQ 不参与权限策略同步；权限仍使用
数据库 version + 周期对账。

这条链路只演示可靠事件发布与幂等消费，不意味着库存、支付已经实现。将来出现
跨服务业务写入时，在 Outbox/Inbox 基础上增加 Saga，而不是引入跨库事务或 2PC。

## 认证与授权

    用户 token
       ↓ 每个服务本地验签
    资源服务 ──gRPC CheckPermission──> admin
       ↓                                  ↓
    业务用例                         本地 Casbin 快照

- 所有服务本地校验 JWT，admin 不成为认证代理。
- 权限要求声明在 proto 的 access / perm 上，handler 不写鉴权分支。
- admin 是策略写入方和判定点；其他服务不连接权限库。
- 远程判定失败时 fail closed，只影响 PERMISSION_REQUIRED 接口；公开或只要求登录的接口不依赖 admin。
- admin 的判定 gRPC 只读，不提供策略写接口；`CheckPermission` 与
  `BatchGetProducts` 是 `INTERNAL` RPC，调用方必须携带 Keycloak
  `client_credentials` 服务令牌。

当判定吞吐成为瓶颈时，可以增加带版本号的本地策略缓存，但不应提前引入 MQ 或把权限表开放给所有服务。

## 同步调用

只有两条同步服务依赖：

- product → admin：权限判定；
- order → product：批量获取下单商品。

客户端在调用方业务模块的 infrastructure，application/domain 不依赖 protobuf。
调用链统一具备短期服务令牌、trace/metadata、客户端指标、熔断和有界重试；只有
两个幂等读 RPC 可以重试。开发环境使用静态地址，Compose/Kubernetes 通过服务
DNS 解析，不嵌入注册中心 SDK。

## 部署拓扑

本地：

    Browser → nginx 开发网关:8000
                ├─ admin
                ├─ product ← order
                └─ order
                     │
            PostgreSQL（每服务独立 database）
              │ Redis / RabbitMQ / MinIO
              └──────── Keycloak

docker compose -f deploy/docker-compose.yml up -d --build 会先执行每个服务的迁移任务，再启动服务和网关。OTel、Prometheus、Tempo、Grafana 通过 --profile obs 按需启动。

生产推荐 Kubernetes：

- 网关/Ingress 只暴露 HTTP；gRPC、metrics 和数据库保持集群内可达；
- 每个服务独立 Deployment、Service、HPA 和 PodDisruptionBudget；
- 迁移使用一次性 Job，成功后再滚动 Deployment；
- 配置进 ConfigMap，DSN/凭据进 Secret；
- file 多副本时把本地 BlobStore 替换为 S3/OSS/MinIO；
- NetworkPolicy 限制 order→product、资源服务→admin 和 Prometheus→metrics。

生产应用基线位于 `deploy/kubernetes/base`，包含 Deployment、Service、Gateway API、
HPA、PDB 与 NetworkPolicy。一次性迁移模板独立位于 `deploy/kubernetes/migrations`，
由发布平台等待成功后再滚动 Deployment。Redis、RabbitMQ、PostgreSQL 和 S3 在生产环境
通过 Secret 接入托管实例，不在应用清单里伪装成单副本生产集群。具体发布顺序见
[生产环境部署](deployment.md)。

## 共享代码边界

pkg 只放无业务语义、能被任意服务使用的技术原语，例如 JWT 验签、授权中间件、健康检查、配置、进程生命周期和传输运行时。业务模型不能进 pkg，服务也不能 import 其他服务拥有的模块；跨服务只 import `api` 契约。

这些约束由 tests/architecture/dependencies_test.go 持续检查。
