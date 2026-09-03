# 部署文件索引

本目录只保存本地开发用的部署资产，不承载完整教程：

- `docker-compose.yml`：PostgreSQL、Keycloak 两个本地依赖，加上应用镜像的一次性迁移任务与常驻服务；
- `postgres/`：初始化脚本，只负责给 Keycloak 建库（业务库由镜像的 `POSTGRES_DB` 创建）；
- `keycloak/`：本地 realm 与 Keycloak 说明（含换成其它 IdP 的配置对照）。

使用说明见[开发环境部署](../docs/development-deployment.md)。

发布单元只有 `eagle` 一个进程。`eagle-migrate` 与它同镜像、不同 entrypoint，
是发布前的一次性迁移任务，不是常驻服务。Compose 里的 `postgres`、`keycloak`
单副本、无备份、凭据写死在文件里，只用于本地，不能带进生产。
