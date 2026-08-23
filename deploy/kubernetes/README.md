# Kubernetes 部署基线

`base/` 是无厂商绑定的应用层基线，包含三个独立服务的 Deployment、Service、
迁移 Job、Gateway API、HPA、PDB、探针、安全上下文和 NetworkPolicy。它不会在
应用命名空间部署单副本 PostgreSQL、Redis、RabbitMQ 或对象存储。

先复制并安全填写 Secret 示例，再创建自己的环境 overlay：

```bash
cp deploy/kubernetes/secret.example.yaml /tmp/eagle-secret.yaml
kubectl apply -f /tmp/eagle-secret.yaml
kubectl kustomize deploy/kubernetes/base
```

不要提交填入真实值的 Secret。生产应优先使用 External Secrets、Vault 或云 Secret
Manager，并在 overlay 中替换镜像、域名、TLS Secret、GatewayClass、OIDC、数据库、
Redis、RabbitMQ 和 S3 地址。

生产服务身份必须分别创建：`eagle-product-worker` 只用于 product 调 admin，
`eagle-order-worker` 只用于 order 调 product。两个 client 都关闭用户登录，仅开启
Client Credentials，并把各自 secret 写入示例中对应的 Secret key；不要复用本地
Compose 的 `eagle-worker`。

推荐发布顺序：

1. 以 Git SHA 构建并推送发生变更的服务镜像；
2. 为本次发布创建名称唯一的迁移 Job；
3. 等迁移 Job 成功，再更新对应 Deployment 镜像；
4. readiness 通过后观察错误预算、延迟、队列积压和消费失败；
5. 使用 expand/contract 完成不兼容数据库变更，禁止依赖启动时自动迁移。

`observability/` 提供 Prometheus Operator 的 ServiceMonitor 和 PrometheusRule：

```bash
kubectl kustomize deploy/kubernetes/observability
```

推荐的生产组合按集群能力选择，不要把两个应用 overlay 重复 apply：

```bash
kubectl apply -k deploy/kubernetes/overlays/production
kubectl apply -k deploy/kubernetes/gateway
kubectl apply -k deploy/kubernetes/observability
kubectl apply -k deploy/kubernetes/backup
```

已安装 Istio 时，用 `overlays/production-mtls` 替代 `overlays/production`。运维闭环和
恢复演练步骤见 `docs/operations.md`。

应用前集群必须已有 Gateway API CRD/控制器和 Prometheus Operator CRD。基线中的
GatewayClass 为 `envoy`，需要按集群实现修改。日志和 trace 后端不在此目录重复部署，
由平台将 Pod stdout 收集到 Loki/云日志，并把 OTLP 发到 Tempo 或兼容后端。

NetworkPolicy 只允许带 `eagle.io/gateway-access=true` 标签的命名空间访问业务 HTTP。
如果 Gateway 控制器的数据面不运行在 `eagle` 命名空间，需要给它所在命名空间补该
标签；可观测性命名空间的 metrics 访问标签已由清单创建。

## 路由与负载均衡

外部 HTTP 流量由 `Gateway` 和 `HTTPRoute` 按路径转发到三个普通 ClusterIP
Service，Service 再把连接分配给就绪 Pod。集群必须预先安装与 `GatewayClass`
匹配的 Gateway API 控制器。

内部 gRPC 使用 `admin-grpc`、`product-grpc` 两个 Headless Service。运行时地址采用
`dns:///...`，DNS resolver 会返回全部就绪 Pod，Kratos gRPC 客户端内置的 `wrr`
balancer 在这些地址之间做请求级负载均衡。不要把地址改回普通 ClusterIP：gRPC
长连接会使负载均衡退化为按连接分配，一个调用方实例可能长期命中同一个 Pod。
