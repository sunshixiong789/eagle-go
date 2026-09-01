# 部署文件索引

本目录只保存部署资产，不承载完整教程：

- `docker-compose.yml`：本地依赖、应用、一次性迁移任务和 nginx 开发网关；
- `postgres/`：初始化脚本，只负责给 Keycloak 建库（业务库由镜像的 `POSTGRES_DB` 创建）；
- `gateway/`：仅供 Compose 使用的本地 nginx 路由；
- `keycloak/`：本地 realm 与 Keycloak 说明（含换成其它 IdP 的配置对照）；
- `observability/`：本地 Prometheus、Alertmanager、Loki、Tempo、Alloy、Grafana 配置。

使用说明按环境拆分：

- [开发环境部署](../docs/development-deployment.md)
- [生产环境部署](../docs/deployment.md)
- [生产运行手册](../docs/operations.md)

发布单元只有 `eagle` 一个进程。`eagle-migrate` 与它同镜像、不同 entrypoint，
是发布前的一次性迁移任务，不是常驻服务。可观测性组件在 `obs` profile 下，
默认不启动。
