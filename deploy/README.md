# 部署文件索引

本目录保存本地开发与 ECS 主机部署资产：

- `docker-compose.yml`：仅供本地开发使用的 PostgreSQL、可选 MySQL profile、迁移任务与应用；
- `compose.app.yml`：远端应用与迁移任务，不包含数据库；
- `scripts/deploy.sh`：云效主机部署入口；
- `environments/*.env.example`：开发、测试环境的非敏感变量示例；
- Google/Apple 登录通过 `EAGLE_AUTH_*` 配置启用，不需要部署独立 IdP。

本地使用见[开发环境部署](../docs/development-deployment.md)，云效配置见
[云效 Flow 开发与测试环境部署](../docs/aliyun-flow-deployment.md)。

发布单元只有 `eagle` 一个进程。`eagle-migrate` 与它同镜像、不同 entrypoint，
是发布前的一次性迁移任务，不是常驻服务。本地 Compose 里的 `postgres` / `mysql`
单副本、无备份、凭据写死在文件里，不能带进远端环境；远端数据库独立管理。
