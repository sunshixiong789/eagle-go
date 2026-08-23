# 部署入口

当前发布单元只有 `admin`、`product`、`order` 三个服务。
它们共用构建模板，但分别生成镜像、运行迁移并独立部署。

- 本地完整环境：`make up`
- 单服务镜像：`make image SERVICE=product VERSION=v1.2.0`
- 全部镜像：`make images VERSION=v1.2.0`
- 编排文件：`docker-compose.yml`
- 本地网关规则：`gateway/nginx.conf`
- Kubernetes 生产基线：`kubernetes/base`
- Kubernetes 可观测性：`kubernetes/observability`
- 详细发布顺序与端口：[../docs/deployment.md](../docs/deployment.md)

Compose 还提供 Redis、RabbitMQ、MinIO，以及 `obs` profile 下的 Prometheus、
Alertmanager、Tempo、Loki、Alloy 和 Grafana。它们用于本地开发和联调，不是生产
高可用方案。生产入口使用 Gateway API，PostgreSQL、Redis、RabbitMQ、对象存储和
可观测性后端应接入托管服务或由独立平台团队维护。

file、notification 是 admin 拥有的模块，不是独立容器。
