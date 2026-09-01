# 开发环境部署

本文只说明本地开发、调试和 IDEA 运行入口。生产镜像、发布顺序和回滚见
[生产环境部署](deployment.md)，线上故障处置见[生产运行手册](operations.md)。

## 两种开发方式

| 方式 | 适用场景 | 应用进程 | 基础依赖 |
|---|---|---|---|
| 完整 Compose | 首次启动、接口联调、验证完整拓扑 | 容器内运行 | Compose 管理 |
| 宿主机调试 | 断点、热重启、日常开发 | IDEA/终端运行 | Compose 只启动依赖 |

Compose 里的 `eagle-migrate` 是一次性数据库迁移任务，`eagle` 才是常驻服务。
迁移容器显示 `Exited (0)` 表示成功，不是异常或重复部署。

`gateway`（nginx）只有一个上游，本地看起来多余。保留它是因为生产入口该有的东西
（TLS 终止、请求体上限、真实客户端 IP）都在这一层，本地带上可以避免「本地能传 50MB、
生产被网关拦掉」这类只在上线时才暴露的差异。

## IDEA 从 Markdown 直接运行

README 和本文中的可执行命令都使用 `bash` 代码块，并遵循「一块一个动作」。IDEA/GoLand 启用
Markdown 和 Shell Script 插件后，代码块左侧会显示运行按钮，可以直接点击执行。

命令默认以仓库根目录为工作目录。需要共享 shell 变量的步骤会合并为一个原子命令；需要持续
交互或手工修改参数的生产命令不伪装成本地一键操作。

## 完整 Compose 环境

首次使用先验证项目锁定的开发工具：

```bash
make init
```

启动完整开发拓扑：

```bash
make up
```

查看常驻服务和一次性任务：

```bash
docker compose -f deploy/docker-compose.yml ps --all
```

预期结果：

- `eagle-migrate` 和 `minio-init` 为 `Exited (0)`；
- `eagle`、`postgres`、`keycloak`、`minio` 为运行状态；
- `gateway` 在 `eagle` 健康后启动。

检查 readiness：

```bash
curl --fail http://127.0.0.1:9101/readyz
```

查看应用日志：

```bash
docker compose -f deploy/docker-compose.yml logs --follow eagle
```

停止环境但保留数据库和对象数据：

```bash
make down
```

`docker compose down -v` 会删除本地数据库和对象数据，不作为入门文档的一键命令。
确实需要重置环境时，应先确认没有要保留的数据。

## 本地入口

| 组件 | 地址 | 本地凭据 |
|---|---|---|
| nginx 开发网关 | `http://127.0.0.1:8000` | - |
| 应用 HTTP（绕过网关） | `http://127.0.0.1:8001` | - |
| 应用 metrics / health | `http://127.0.0.1:9101` | - |
| Keycloak | `http://127.0.0.1:8080` | 管理员 `admin/admin` |
| MinIO API | `http://127.0.0.1:9004` | `eagle/eagle-local-secret` |
| MinIO Console | `http://127.0.0.1:9005` | `eagle/eagle-local-secret` |
| Grafana（obs profile） | `http://127.0.0.1:3000` | 匿名 Admin |
| Prometheus（obs profile） | `http://127.0.0.1:9090` | - |

容器之间使用 Compose DNS（`eagle:8000`、`postgres:5432`、`minio:9000`）；
宿主机进程使用上表中的 `127.0.0.1` 端口。

## 宿主机断点调试

只启动 PostgreSQL、Keycloak 和 MinIO：

```bash
make up-deps
```

执行迁移：

```bash
make migrate-up
```

从 Markdown 运行或在终端启动应用：

```bash
make run
```

本地配置在 `configs/config.yaml`。宿主机运行使用其中的 `127.0.0.1` 默认值；容器运行由
Compose 的 `EAGLE_*` 环境变量覆盖为容器 DNS。

不要同时运行 `eagle` 容器和宿主机进程，否则 HTTP 与 metrics 端口会冲突。需要断点调试时
先停掉容器：

```bash
docker compose -f deploy/docker-compose.yml stop eagle
```

## 可观测性联调

在完整环境基础上启动可观测性 profile：

```bash
docker compose -f deploy/docker-compose.yml --profile obs up -d
```

该 profile 启动 Prometheus、Alertmanager、Tempo、Loki、Alloy、Grafana 和 OTel Collector。
Alloy 从 Docker stdout 收集结构化日志；应用把 trace 发到 OTel Collector。没有启动 profile 时，
OTLP exporter 在后台重试，不会阻止应用启动。

## 首个本地用户

`deploy/keycloak/realm-eagle.json` 刻意不预置任何用户——预置用户就等于把一份可用凭据提交
进仓库。本地用户手工创建。

登录 Keycloak 管理 CLI：

```bash
docker compose -f deploy/docker-compose.yml exec -T keycloak /opt/keycloak/bin/kcadm.sh config credentials --server http://localhost:8080 --realm master --user admin --password admin
```

创建用户：

```bash
docker compose -f deploy/docker-compose.yml exec -T keycloak /opt/keycloak/bin/kcadm.sh create users -r eagle -s username=alice -s enabled=true -s firstName=Alice -s lastName=Test -s email=alice@example.com
```

设置密码：

```bash
docker compose -f deploy/docker-compose.yml exec -T keycloak /opt/keycloak/bin/kcadm.sh set-password -r eagle --username alice --new-password 'Passw0rd!'
```

授予开发管理员角色。这里加的是 `eagle-api` 这个 client 上的角色，不是 realm 角色——
超管短路只认 client 角色：

```bash
docker compose -f deploy/docker-compose.yml exec -T keycloak /opt/keycloak/bin/kcadm.sh add-roles -r eagle --uusername alice --cclientid eagle-api --rolename admin
```

获取 token 并调用权限接口。这个代码块是一个原子动作，避免 IDEA 分别运行代码块时丢失
shell 变量：

```bash
TOKEN=$(curl --silent --fail -d client_id=eagle-web -d username=alice -d 'password=Passw0rd!' -d grant_type=password http://127.0.0.1:8080/realms/eagle/protocol/openid-connect/token | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p') && curl --fail -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8000/v1/system/permissions
```

不带 `Authorization` 应返回 401；带无权限的 token 应返回 403。

## 常见问题

### `eagle-migrate` 一直运行或退出码非 0

正常迁移会很快结束并显示 `Exited (0)`。先查看日志：

```bash
docker compose -f deploy/docker-compose.yml logs eagle-migrate
```

常见原因是 PostgreSQL 尚未健康、数据卷来自旧版本，或迁移 SQL 失败。不要绕过迁移任务强行
启动应用——应用启动时会校验 proto 声明的权限码与数据库 catalog 一致，迁移没跑完会直接
fail closed。

### 修改迁移后应用起不来

脚手架的历史迁移可能被就地修改过。本地数据卷里记录的版本与文件不一致时，重建数据卷：

```bash
docker compose -f deploy/docker-compose.yml down -v
```

### Markdown 代码块没有运行按钮

确认项目是从仓库根目录打开，并启用了 Markdown 和 Shell Script 插件。代码块语言必须是
`bash`；纯文本、YAML、配置示例和生产占位命令不会提供一键执行入口。

### 接口返回 401

`EAGLE_AUTH_ISSUER` 必须与 token 里的 `iss` 逐字一致。Compose 里 issuer 用的是宿主机可见的
`http://127.0.0.1:8080/...`，而 JWKS 走容器网络 `http://keycloak:8080/...`——两者地址不同是
故意的，改成一致反而会 401。

### 接口返回 403

先确认角色加在 `eagle-api` client 上而不是 realm 上。其次，权限策略每 5 秒对账一次，
刚改完可能需要等一个周期。
