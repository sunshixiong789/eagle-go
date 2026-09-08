# 部署文件索引

本目录保存本地开发与 ECS 主机部署资产：

- `docker-compose.yml`：仅供本地开发使用的 PostgreSQL、可选 MySQL profile、迁移任务与应用；
- `compose.app.yml`：远端应用与迁移任务，不包含数据库；
- `scripts/ci.sh`：云效质量门禁，可拆分 check / test / postgres / mysql 任务；
- `scripts/build-release.sh`：Buildx 构建并推送 ACR，生成包含镜像 digest 的发布包；
- `scripts/cd.sh`：云效主机部署入口，读取发布包，支持 rollback；
- `scripts/deploy.sh`：环境锁、迁移、健康检查和应用快照回滚；
- `scripts/registry-login.sh`：共享 ACR 登录步骤，支持主机已有凭据；
- `tests/test_deploy.py`：无 Docker 的发布故障回归，运行 `make test-deploy`；
- `environments/*.env.example`：开发、测试环境的非敏感变量示例；
- Google/Apple 登录通过 `EAGLE_AUTH_*` 配置启用；JWT 私钥以只读 Secret 目录挂载，公钥由 JWKS 端点发布。

本地使用见[开发环境部署](../docs/development-deployment.md)，云效配置见
[云效 Flow CI/CD 与部署](../docs/aliyun-flow-deployment.md)。

发布单元只有 `eagle` 一个进程。`eagle-migrate` 与它同镜像、不同 entrypoint，
是发布前的一次性迁移任务，不是常驻服务。本地 Compose 里的 `postgres` / `mysql`
单副本、无备份、凭据写死在文件里，不能带进远端环境；远端数据库独立管理。
