# 打包与部署

## 发布单元

模块不等于微服务。当前六个模块只组合成三个可独立发布的进程：

| 服务 | 组合的模块 | 二进制 | 镜像 | 数据库 |
|---|---|---|---|---|
| admin | access、dictionary、file、notification | `bin/admin` | `eagle/admin:<version>` | `eagle_admin` |
| product | product | `bin/product` | `eagle/product:<version>` | `eagle_product` |
| order | order | `bin/order` | `eagle/order:<version>` | `eagle_order` |

`app/<service>/cmd/<service>` 是进程的唯一组合根，`app/<service>/migrations` 是它独占的迁移目录。文件和通知仍是独立业务模块，但当前没有独立扩缩容、团队或故障隔离需求，所以不单独部署。

## 本地二进制

编译全部服务：

```bash
make build
./bin/admin -conf app/admin/configs
./bin/product -conf app/product/configs
./bin/order -conf app/order/configs
```

也可以只运行一个服务：

```bash
make migrate-up SERVICE=product
make run SERVICE=product
```

## 镜像打包

仓库使用一个参数化的多阶段 Dockerfile，但产物是三个互不相同的镜像，
不是一个镜像启动不同命令。服务在隔离的 Go builder 阶段编译，运行阶段
只使用 distroless；因此发布不依赖构建机本地安装的 Go 版本或 CPU 架构。

单独构建商品服务：

```bash
make image SERVICE=product VERSION=v1.2.0 REGISTRY=registry.example.com/eagle
```

一次构建并推送全部服务：

```bash
make images VERSION=v1.2.0 REGISTRY=registry.example.com/eagle
make push-images VERSION=v1.2.0 REGISTRY=registry.example.com/eagle
```

等价的原始命令是：

```bash
docker build --build-arg SERVICE=product --build-arg VERSION=v1.2.0 \
  -t registry.example.com/eagle/product:v1.2.0 .
```

镜像内同时带有 `/app/service`、`/app/migrate`、`/app/healthcheck` 和仅属于
当前服务的 `/app/migrations`。不会把其他服务的二进制或迁移打进镜像。
BuildKit 会缓存 Go module 与编译结果，但每次只构建 `SERVICE` 指定的服务。

## Docker Compose 开发拓扑

```text
Browser -> nginx:8000
             |-- admin:8000   -> eagle_admin
             |-- product:8000 -> eagle_product
             `-- order:8000   -> eagle_order

product --gRPC CheckPermission--> admin
order   --gRPC BatchGet--------> product
```

启动完整环境：

```bash
make up
docker compose -f deploy/docker-compose.yml ps
```

Compose 会先运行 `admin-migrate`、`product-migrate`、`order-migrate`，迁移成功后
再启动对应服务。product 等待 admin readiness，order 等待 product readiness，
三个服务全部就绪后才启动 nginx 网关。可观测性组件按需启动：

```bash
docker compose -f deploy/docker-compose.yml --profile obs up -d
```

端口如下：

| 服务 | HTTP | gRPC | metrics |
|---|---:|---:|---:|
| gateway | 8000 | - | - |
| admin | 8001 | 9001 | 9101 |
| product | 8002 | 9002 | 9102 |
| order | 8003 | 9003 | 9103 |

容器之间使用 `admin:9000`、`product:9000` 这样的服务 DNS，宿主机映射端口只用于调试。

## 生产部署

每个镜像对应一个独立 Kubernetes Deployment 和 ClusterIP Service。发布顺序固定为：

1. 用 Git SHA 或版本号构建三个不可变镜像；
2. 只对发生变更的服务运行 `/app/migrate -dir /app/migrations` Job；
3. Job 成功后滚动该服务的 Deployment；
4. `/readyz` 通过后接入流量；
5. 观察错误率、延迟和授权判定失败。

生产环境差异很大的镜像仓库、域名、证书、Secret 管理器和 StorageClass 不在底座中伪造默认值。部署平台应提供：

| 能力 | Kubernetes 对象 |
|---|---|
| 服务进程 | Deployment、Service、PodDisruptionBudget |
| 外部 HTTP | Ingress 或 Gateway API |
| 普通配置 | ConfigMap |
| DSN、OIDC、对象存储凭据 | Secret / ExternalSecret |
| 数据库变更 | 每次发布的一次性 Job |
| 弹性 | 有真实容量数据后配置 HPA |
| 网络隔离 | NetworkPolicy |
| 可观测性 | ServiceMonitor、OTLP exporter |

数据库迁移采用 expand/contract。Deployment 容器启动时不自动迁移，避免多副本同时迁移。

## 网络与存储边界

| 来源 | 目标 | 用途 |
|---|---|---|
| Ingress/Gateway | 三个服务 HTTP | 外部 API |
| product | admin gRPC | 权限判定 |
| order | product gRPC | 商品快照 |
| 各服务 | 自己的 PostgreSQL database | 数据访问 |
| 各服务 | Keycloak JWKS | token 验签 |
| Prometheus | 各服务 metrics | 指标采集 |
| 各服务 | OTel Collector | trace 上报 |

admin 当前使用本地 BlobStore，只适合开发或单副本。生产多副本前实现已有 `BlobStore` 端口，对接 S3、OSS 或 MinIO；这属于存储适配器替换，不需要再拆一个文件微服务。

只有文件流量需要独立扩容、通知出现独立异步消费链路，或模块由不同团队独立发布时，才把对应模块提升为新服务。
