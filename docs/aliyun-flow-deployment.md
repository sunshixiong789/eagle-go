# 云效 Flow 开发与测试环境部署

本方案面向阿里云 ECS + Docker Compose。数据库作为独立基础设施部署，可使用 RDS PostgreSQL 或 RDS MySQL；
ECS 上只运行一次性迁移任务和 `eagle` 应用。若后续迁移到 ACK，仍沿用“一个镜像、先迁移、后发布”
的边界，但应把本清单替换为 Job + Deployment。

## 推荐拓扑

| 资源 | development | testing |
|---|---|---|
| 计算 | 开发 ECS 主机组 | 测试 ECS 主机组 |
| 数据库 | 独立 RDS 数据库与账号 | 独立 RDS 数据库与账号 |
| 认证 | 独立 JWT audience / 第三方登录客户端 | 独立 JWT audience / 第三方登录客户端 |
| 云效变量组 | `eagle-development` | `eagle-testing` |
| 发布 | `develop` 分支自动 | 人工卡点后提升同一镜像 |

两个环境至少要使用不同数据库和账号；更推荐使用不同 RDS 实例。RDS 只开放给对应 ECS 安全组，
不映射公网端口。数据库账号只授予该环境数据库的权限；PostgreSQL 使用 `sslmode=require` 或更强，MySQL DSN 启用 TLS。
备份、监控、参数组与版本升级由 RDS 管理，不再放入应用 Compose 生命周期。

`deploy/docker-compose.yml` 继续只服务本地开发，其中的 PostgreSQL/MySQL 不是远端部署资产。远端使用
`deploy/compose.app.yml`，该文件没有数据库服务。

## 流水线设计

建议把质量检查和发布拆成以下阶段：

1. **检查**：运行 `make build`、`go vet ./...`、`go -C tools vet ./...`、`make test` 和 `make lint`。
2. **构建镜像**：使用仓库根目录 `Dockerfile` 构建并推送到 ACR。镜像标签使用提交 SHA，禁止
   `latest`，例如 `registry.cn-hangzhou.aliyuncs.com/acme/eagle:<commit-sha>`；在镜像构建任务中
   把代码源的提交 SHA 映射到标签即可。
3. **部署 development**：关联 `eagle-development` 变量组，选择开发 ECS 主机组；`develop`
   分支成功后自动执行。
4. **验证 development**：从网关或 ECS 访问 `/readyz`，并执行必要的 API 冒烟测试。
5. **人工卡点**：只有通过开发环境验证的镜像才能进入测试环境。
6. **部署 testing**：关联 `eagle-testing` 变量组，选择测试 ECS 主机组，直接使用第 2 步的
   `EAGLE_IMAGE`，不要重新构建。

如果团队要求测试环境由 release 分支触发，也应把“构建”和“环境部署”拆成流水线：上游产出
带 SHA 或 digest 的镜像地址，下游只接收该不可变地址。这样重试和提升不会得到不同二进制。

## 云效变量

在 Flow 创建两个通用变量组，并在对应部署任务中关联。敏感项开启“私密模式”。同名变量由环境
变量组提供，不要把密码写进仓库、制品或部署命令文本。

| 变量 | 类型 | 说明 |
|---|---|---|
| `DEPLOY_ENV` | 普通 | `development` 或 `testing` |
| `EAGLE_IMAGE` | 上游输出 | ACR 完整镜像地址，使用提交 SHA/digest |
| `EAGLE_DATABASE_DRIVER` | 普通 | `postgres` 或 `mysql` |
| `EAGLE_DATABASE_DSN` | 私密 | 当前环境独立 RDS DSN |
| `EAGLE_AUTH_ISSUER` | 普通 | 必须与 token 的 `iss` 完全一致 |
| `EAGLE_AUTH_AUDIENCE` | 普通 | 当前环境 Eagle JWT audience |
| `EAGLE_AUTH_SIGNING_SECRET` | 私密 | 至少 32 字节的随机 Eagle token 签名密钥 |
| `EAGLE_AUTH_GOOGLE_ENABLED` / `EAGLE_AUTH_GOOGLE_CLIENT_ID` | 普通 | 启用 Google 登录及 Client ID |
| `EAGLE_AUTH_APPLE_ENABLED` / `EAGLE_AUTH_APPLE_CLIENT_ID` | 普通 | 启用 Apple 登录及 Services ID / Bundle ID |
| `EAGLE_OBSERVABILITY_OTLP_ENDPOINT` | 普通 | 可选，环境自己的 collector |
| `EAGLE_BIND_ADDRESS` | 普通 | 默认 `127.0.0.1`，由同机网关反代；直连时按网络设计调整 |
| `EAGLE_HTTP_PORT` / `EAGLE_METRICS_PORT` | 普通 | 默认 `8000` / `9101` |

完整的非敏感建议值见 `deploy/environments/*.env.example`。开发和测试部署到同一台 ECS 虽然可用
不同端口实现，但会共享故障域和资源，不建议作为长期方案。
DSN 中的用户名、密码必须进行 URL 百分号编码；部署变量值不接受原始单引号或换行。

## 云效任务配置

“镜像构建并推送至 ACR”任务把完整镜像地址设置为下游变量 `EAGLE_IMAGE`。主机部署任务需要把
下面两个文件作为制品一同下发，且保持目录结构：

```text
deploy/compose.app.yml
deploy/scripts/deploy.sh
```

在开发、测试主机组分别预装 Docker Engine、Docker Compose v2 与 `flock`（通常由 `util-linux`
提供），并给执行用户访问 Docker 的权限。主机部署命令在制品解压目录执行：

```bash
chmod +x deploy/scripts/deploy.sh
deploy/scripts/deploy.sh
```

脚本会依次执行：校验变量 → 拉取镜像 → 用同一镜像执行迁移 → 更新应用 → 等待 readiness。
迁移失败时旧实例保持运行；新实例 readiness 失败时脚本恢复上一个应用镜像并让流水线失败。
同一环境的并发发布会被文件锁拒绝，避免两个流水线互相覆盖运行配置。
数据库迁移不会自动回滚，因此迁移必须采用 expand/contract：先做向后兼容的扩展，旧字段至少保留
一个发布周期，再在后续版本清理。

每个环境的运行文件保存在 `/opt/eagle/<environment>`，其中 `runtime.env` 权限为 `0600`。
如果部署用户不能写 `/opt/eagle`，在变量组把 `EAGLE_DEPLOY_ROOT` 设置为该用户专用的绝对目录。

## 首次上线检查

- RDS 数据库和最小权限账号已按环境分别创建，ECS 到 RDS 的私网连通性正常。
- ACR 凭据通过云效服务连接或主机凭据助手管理，部署日志不打印口令。
- 开发与测试使用不同主机组、变量组、数据库、JWT audience、第三方登录客户端和观测标签。
- 入口网关完成 TLS、请求限制和真实客户端 IP 传递，只暴露业务端口；metrics 端口仅监控网络可达。
- Flow 测试部署阶段配置人工卡点，且复用开发环境已验证的镜像地址。
- 为 RDS 启用自动备份，并对 `/readyz`、错误率、延迟和数据库连接池设置告警。

云效界面操作可参考阿里云官方的[主机 Docker 部署](https://help.aliyun.com/zh/yunxiao/user-guide/host-docker-deployment)、
[主机部署](https://help.aliyun.com/zh/yunxiao/user-guide/host-deployment-1)和
[通用变量组](https://help.aliyun.com/zh/yunxiao/user-guide/common-variable-group)文档。
