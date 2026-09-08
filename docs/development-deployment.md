# 开发环境部署

本文只说明本地开发、调试和 IDEA 运行入口。模块边界见[架构说明](architecture.md)。

## 两种开发方式

| 方式 | 适用场景 | 应用进程 | 基础依赖 |
|---|---|---|---|
| 完整 Compose | 首次启动、接口联调、验证完整拓扑 | 容器内运行 | Compose 管理 |
| 宿主机调试 | 断点、热重启、日常开发 | IDEA/终端运行 | Compose 只启动依赖 |

Compose 里的 `eagle-migrate` 是一次性数据库迁移任务，`eagle` 才是常驻服务。
迁移容器显示 `Exited (0)` 表示成功，不是异常或重复部署。

## IDEA 从 Markdown 直接运行

README 和本文中的可执行命令都使用 `bash` 代码块，并遵循「一块一个动作」。IDEA/GoLand 启用
Markdown 和 Shell Script 插件后，代码块左侧会显示运行按钮，可以直接点击执行。

命令默认以仓库根目录为工作目录。需要共享 shell 变量的步骤会合并为一个原子命令。

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

- `eagle-migrate` 为 `Exited (0)`；
- `eagle`、`postgres` 为运行状态。

检查 readiness：

```bash
curl --fail http://127.0.0.1:9101/readyz
```

查看应用日志：

```bash
docker compose -f deploy/docker-compose.yml logs --follow eagle
```

停止环境但保留数据库数据：

```bash
make down
```

`docker compose down -v` 会删除本地数据库数据，不作为入门文档的一键命令。
确实需要重置环境时，应先确认没有要保留的数据。

## 本地入口

| 组件 | 地址 |
|---|---|
| 应用 HTTP | `http://127.0.0.1:8000` |
| 应用 metrics / health | `http://127.0.0.1:9101` |

容器内通过 Compose DNS `postgres:5432` 访问数据库；宿主机进程使用 `127.0.0.1:5432`。
Google/Apple 是登录时的外部身份提供方；Eagle 验证其 ID Token 后维护本地会话并签发自己的 JWT。

## 宿主机断点调试

只启动 PostgreSQL：

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

调试 MySQL 时使用 Compose 中的 `mysql` profile，三个命令都显式传入方言：

```bash
make up-deps EAGLE_DATABASE_DRIVER=mysql
make migrate-up EAGLE_DATABASE_DRIVER=mysql
make run EAGLE_DATABASE_DRIVER=mysql
```

本地配置在 `configs/config.yaml`。宿主机运行使用其中的 `127.0.0.1` 默认值；容器运行由
Compose 的 `EAGLE_*` 环境变量覆盖为容器 DNS。

宿主机进程与 `eagle` 容器使用同一组宿主机端口（8000 / 9101），不能同时运行。需要断点调试时
先停掉容器：

```bash
docker compose -f deploy/docker-compose.yml stop eagle
```

## 可观测性

应用输出结构化 JSON 日志到 stdout，宿主机的指标与健康检查默认在 `9101` 端口
（`/metrics`、`/livez`、`/readyz`）。容器内仍使用 `9100`。trace 默认不上报；有 OTLP collector 时通过
`EAGLE_OBSERVABILITY_OTLP_ENDPOINT=host:4317` 打开，collector 不可达只会在后台重试，
不会阻止应用启动。

## 接入 Google / Apple 登录

在 Google Cloud 或 Apple Developer 创建客户端，通过 `EAGLE_AUTH_GOOGLE_*` / `EAGLE_AUTH_APPLE_*`
启用登录。客户端 SDK 取得 ID Token 后调用 `/v1/auth/social/login`，再用响应中的 Eagle access token
调用接口。完整请求格式见[Google / Apple 登录](social-login.md)。

```bash
curl --fail -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8000/v1/system/permissions
```

不带 `Authorization` 应返回 401；带无权限的 token 应返回 403。

## 常见问题

### 本机端口已被占用

Compose 的业务端口和可观测端口都可以覆盖：

```bash
EAGLE_HTTP_PORT=18000 EAGLE_METRICS_PORT=19101 make up
```

Go builder 默认从 Google 的 Docker Hub 公共缓存拉取，避免部分网络下 Docker Hub
代理返回 403。需要改回 Docker Hub 官方地址或使用内部镜像时可显式覆盖：

```bash
EAGLE_BUILDER_IMAGE=golang:1.27-alpine make up
```

### `eagle-migrate` 一直运行或退出码非 0

正常迁移会很快结束并显示 `Exited (0)`。先查看日志：

```bash
docker compose -f deploy/docker-compose.yml logs eagle-migrate
```

常见原因是所选数据库尚未健康、数据卷来自旧版本，或迁移 SQL 失败。不要绕过迁移任务强行
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

`EAGLE_AUTH_ISSUER` 与 `EAGLE_AUTH_AUDIENCE` 必须和 Eagle 自己签发的 token 完全一致，所有副本也必须共享同一个签名 Secret。

### 接口返回 403

确认 Eagle token 中包含预期的普通角色键，并检查该角色是否通过 Casbin 绑定了接口声明的权限码。
权限策略每 5 秒对账一次，刚改完可能需要等一个周期。
