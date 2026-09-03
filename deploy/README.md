# 部署文件索引

本目录只保存本地开发用的部署资产，不承载完整教程：

- `docker-compose.yml`：PostgreSQL、本地一次性迁移任务与应用服务；
- 外部 IdP 不属于本仓库部署拓扑，通过 `EAGLE_AUTH_*` 配置接入。

使用说明见[开发环境部署](../docs/development-deployment.md)。

发布单元只有 `eagle` 一个进程。`eagle-migrate` 与它同镜像、不同 entrypoint，
是发布前的一次性迁移任务，不是常驻服务。Compose 里的 `postgres`
单副本、无备份、凭据写死在文件里，只用于本地，不能带进生产。
