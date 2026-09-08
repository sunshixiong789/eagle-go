# 云效 Flow CI/CD 与部署

发布链路为：**云效质量检查 → Buildx 构建并推送 ACR → 上传发布包 → ECS 迁移 → 应用健康检查**。
开发、测试、生产环境复用同一个 Docker 镜像 digest 和发布包，不在 ECS 编译代码。数据库由环境独立管理，
远端只运行 `eagle` 和发布时的一次性迁移容器。

当前脚本面向 **每个环境一台 Linux ECS + Docker Compose**，原地替换容器会有短暂中断。
文件锁只保护当前主机；不要把该脚本并行下发到共享数据库的多台 ECS。需要多副本和无中断发布时，
再引入单独的迁移任务与负载均衡滚动发布。

## 仓库入口

| 脚本 | 执行位置 | 职责 |
|---|---|---|
| `deploy/scripts/ci.sh all` | 云效构建机 | 完整 CI，任一步失败则停止 |
| `deploy/scripts/build-release.sh` | 云效构建机 | 构建、推送 ACR，生成无密钥的发布包 |
| `deploy/scripts/cd.sh` | ECS 制品解压目录 | 从发布包读取 digest，执行部署 |
| `deploy/scripts/cd.sh rollback` | ECS 制品解压目录 | 切回主机保存的上一成功快照 |
| `deploy/scripts/deploy.sh` | ECS | 实现环境锁、快照、迁移和失败回滚 |

仓库的 GitHub Actions 只保留 `workflow_dispatch` 手动检查，不再自动触发。

## 环境配置文件

应用交付物是 ACR 中的 Docker 镜像；所有环境复用镜像内的 **`configs/config.yaml` 共用模板**。
发布包只传递 Compose、脚本和镜像地址，模板随镜像版本固定，不需要按环境复制或挂载 YAML。
各环境的差异在云效变量组中维护：

| 用途 | 配置来源 | 日志 / trace 采样率建议值 |
|---|---|---|
| 本地开发 | 模板开发默认值 | info / 1.0 |
| 开发 ECS | `eagle-development` 变量组 | debug / 1.0 |
| 测试 ECS | `eagle-testing` 变量组 | info / 0.1 |
| 生产 ECS | `eagle-production` 变量组 | info / 0.05 |

每条 CD 流水线只关联自己的变量组，设置 `DEPLOY_ENV`。CD 将该环境的运行变量保存到 `runtime.env`
并通过 Compose `env_file` 注入容器；应用继续以 `-conf /app/configs` 读取共用模板。
`DEPLOY_ENV` 只选择部署目录、Compose project 与锁，不负责在云效自动切换变量组。
未知环境或缺少 DSN、issuer、audience、签名 key ID 时部署失败，禁止远端使用本地身份和数据库默认值。

环境差异通过 `EAGLE_*` 变量注入，优先级为 **显式环境变量 > 共用模板默认值**。
可选变量未设置时使用模板默认值。`deploy/environments/*.env.example` 是云效变量组的填写示例，
不作为应用配置加载，也不会进入发布包。调整模板结构或全环境默认值时才修改 `configs/config.yaml`。
数据库 driver 统一通过 `EAGLE_DATABASE_DRIVER`（默认 postgres）控制，应用和迁移读取同一值。
JWT audience 必填，建议分别设置为 `eagle-api-development`、`eagle-api-testing`、`eagle-api-production`。
连接池可设置 `EAGLE_DATABASE_MAX_CONNS`、`EAGLE_DATABASE_MAX_IDLE_CONNS`、
`EAGLE_DATABASE_MAX_CONN_LIFETIME`、`EAGLE_DATABASE_MAX_CONN_IDLE_TIME`；HTTP 超时使用
`EAGLE_SERVER_HTTP_TIMEOUT`，时长均用秒格式，例如 `8s`、`3600s`。

回滚会恢复旧镜像（包含旧模板）、运行变量和 Compose，不受当前云效变量组新值影响。
同机运行多个环境时还要设置不同的宿主机端口、数据库与密钥目录；生产建议独立 ECS。

## 1. 准备构建环境

建议使用云效 **默认 VM 环境或私有 Linux 构建机**，预装：

- 与 `go.mod` / `tools/go.mod` 一致的 Go（当前 1.27.0）、Git、Make、C 编译器（race detector）、Python 3。
- Docker Engine、Buildx 和 Docker Compose v2，执行用户可以访问本机 Docker daemon。
- 可访问 Go module proxy、Buf Registry、embedded-postgres 二进制下载源及 ACR。
- **非 root 构建用户**：`ci.sh test` 使用 embedded-postgres，PostgreSQL 的 `initdb` 拒绝 root。

CI 的临时 PostgreSQL/MySQL 将端口随机绑定在构建机 `127.0.0.1`。构建命令与 Docker daemon
必须共享主机网络；普通隔离的构建容器连不到宿主机 loopback 时，请改用 VM/私有构建机。
不要传入远端环境的数据库账号：CI 自己创建临时数据库容器，退出时删除。

如果执行命令默认是 root，在**专用构建机**预建可访问 Docker 的 `eagle-ci` 用户，
并把云效工作目录安排在该用户能访问的位置。让 checkout 和命令步骤都由该用户运行；
已有 root checkout 时，可在构建任务中切换用户运行（工作目录及缓存须可读写）：

```bash
runuser -u eagle-ci -- sh deploy/scripts/ci.sh all
```

推荐缓存 `go env GOMODCACHE`、`go env GOCACHE` 对应目录和构建用户的
`~/.embedded-postgres-go`，缓存键包括 OS、架构、Go 版本与两个 `go.sum`。
`bin/` 不跨提交缓存，避免工具版本改变后仍使用旧二进制。国内构建机可以通过
`EAGLE_BUILDER_IMAGE`、`EAGLE_RUNTIME_IMAGE` 指定事先同步到 ACR 的基础镜像，保持原镜像内容和版本。

云效构建环境和 Docker Daemon 的配置见[官方构建集群文档](https://help.aliyun.com/zh/yunxiao/user-guide/build-a-cluster)。

## 2. CI 流水线

在 Flow 获取代码步骤拉取完整 Git 历史和目标分支，设置普通变量：

```text
EAGLE_CI_BASE_REF=origin/master
```

按实际主分支调整为 `origin/main` 或 `origin/develop`。MR 使用目标分支；脚本计算 merge-base
后检查已存在迁移不可改写和 API 兼容性。主分支合入后的流水线应传入合入前的已验证提交 SHA，
不要拿当前 HEAD 自己作为基线。缺失变量或未拉取基线都会失败，不静默跳过检查。

Flow「执行命令」步骤，在代码仓库根目录运行：

```bash
sh deploy/scripts/ci.sh all
```

`all` 顺序执行以下门禁，也可分别用子命令配置独立任务：

| 子命令 | 检查 |
|---|---|
| `check` | 迁移不可改写、Buf lint/breaking、API/配置/Ent 生成一致性、构建、两个 module 的 vet、golangci-lint、Compose 解析、发布脚本故障回归 |
| `test` | 核心单测覆盖率至少 80%、业务 module race + PostgreSQL 集成/e2e、tools module 测试 |
| `postgres` | 临时 PostgreSQL 上执行迁移 up → down-to 0 → up |
| `mysql` | 临时 MySQL 上执行仓储/e2e 测试和迁移 up → down-to 0 → up |

拆分任务时每个任务都要 checkout，全部成功才能进入构建。可上传
`dist/coverage/eagle.out` 作为覆盖率制品。MR 流水线运行到 CI 即可，不授予 ACR 推送或 ECS 部署凭据。

## 3. 构建并上传发布包

在受保护发布分支（例如 `develop`）的流水线，CI 成功后增加「执行命令」和「构建物上传」步骤。
命令步骤和上传步骤必须在同一个任务，确保共享工作区。

构建变量：

| 变量 | 必需 | 说明 |
|---|---|---|
| `EAGLE_IMAGE_REPOSITORY` | 是 | 如 `registry.cn-hangzhou.aliyuncs.com/acme/eagle`，不带标签 |
| `CI_COMMIT_SHA` | Flow 提供 | 完整 SHA，脚本会与 checkout 的 HEAD 核对；多代码源需映射正确源的 SHA |
| `BUILD_NUMBER` | Flow 提供 | 与 SHA 一起构成可追踪标签；部署使用 digest |
| `EAGLE_BUILD_PLATFORM` | 否 | 默认 `linux/amd64`，ARM ECS 设置 `linux/arm64`，构建机须支持对应平台 |
| `EAGLE_BUILDER_IMAGE` / `EAGLE_RUNTIME_IMAGE` | 否 | 可覆盖 Dockerfile 的基础镜像地址，生产建议固定 digest |
| `GOPROXY` | 否 | 覆盖镜像内的 Go module proxy |
| `EAGLE_ACR_REGISTRY` | 使用密码登录时 | ACR registry 域名 |
| `EAGLE_ACR_USERNAME` / `EAGLE_ACR_PASSWORD` | 使用密码登录时 | Flow 私密变量；脚本通过 `--password-stdin` 登录 |

也可用当前执行用户预配置的 Docker credential helper / 云效登录步骤。云效服务连接只有在相应
登录步骤实际给当前 Docker 客户端配置凭据后才生效，单纯关联服务连接不会让任意 Shell 自动登录。
共享构建机应为每个任务配置独立 `DOCKER_CONFIG`，部署机使用只有拉取权限的凭据。

执行命令：

```bash
sh deploy/scripts/build-release.sh
```

脚本拒绝未提交的源码，使用 Buildx 的 `containerimage.digest` 元数据绑定刚推送的构建结果。
它不通过重新查询可变标签推断 digest。元数据机制见
[Docker Buildx 官方文档](https://docs.docker.com/reference/cli/docker/buildx/build/#metadata-file)。

配置「构建物上传」：制品名称 `eagle-release`，上传路径 **`dist/eagle-release.tgz`**，仅成功时上传。
该 tar 包内部为：

```text
deploy/compose.app.yml
deploy/scripts/cd.sh
deploy/scripts/deploy.sh
deploy/scripts/registry-login.sh
release/image.txt                 # repository@sha256:...
release/commit.txt                # 完整 Git SHA
```

`dist/release-image.txt` 可用于展示镜像地址；有 `FLOW_ENV` 时也会追加 `EAGLE_IMAGE` 供后续步骤使用。
**CD 始终从发布包读镜像地址**，因此不依赖跨任务 Shell export。若流水线另外注入的
`EAGLE_IMAGE` 与包内地址不同，CD 会失败，防止发布错版本。
云效的变量传递与多代码源规则见[官方环境变量文档](https://help.aliyun.com/zh/yunxiao/user-guide/environment-variables/)。

## 4. 环境与 ECS

| 资源 | development | testing | production |
|---|---|---|---|
| 计算 | 开发 ECS，单台 | 测试 ECS，单台 | 生产 ECS，单台 |
| 数据库 | 开发数据库与账号 | 测试数据库与账号 | 生产数据库与账号 |
| JWT | 开发密钥、issuer / audience | 测试密钥、issuer / audience | 生产密钥、issuer / audience |
| Flow 变量组 | `eagle-development` | `eagle-testing` | `eagle-production` |
| 发布入口 | CI 成功后自动 | 开发验证后人工提升 | 测试验证后人工提升 |

建议分别创建开发、测试、生产 CD 流水线，每条只关联自己的变量组。不要在同一流水线关联多组同名
环境变量并假定它们自动按阶段隔离；Flow 同名变量存在覆盖优先级。

ECS 预装 Docker Engine、Docker Compose v2（支持 `up --wait`）、`flock`（util-linux）、
`install`、`mktemp` 和 tar。部署用户需要 Docker 权限，以及 `/opt/eagle` 下环境目录的写入权限。
也可通过 `EAGLE_DEPLOY_ROOT` 设置该用户专用的绝对路径。

在 ECS 密钥目录放置 `<kid>.pem`。默认 distroless nonroot 的 UID/GID 为 **65532**
（[上游定义](https://github.com/GoogleContainerTools/distroless/blob/main/common/variables.bzl)），
私钥应同时允许部署用户检查和容器用户读取，例如 root 部署时：

```bash
install -d -o root -g 65532 -m 0750 /opt/eagle/secrets/testing-jwt
# 将环境私钥安全地放入目录后设置权限；不要使用仓库本地开发密钥。
chown root:65532 /opt/eagle/secrets/testing-jwt/test-key.pem
chmod 0640 /opt/eagle/secrets/testing-jwt/test-key.pem
```

非 root 部署用户可设为目录/文件 owner，保留容器可读的组权限。签名密钥目录只读挂载，
私钥不进入镜像、制品或 Flow 日志。密钥轮换期间保留回滚版本所需旧 key 文件，文件名与密钥内容不要复用覆盖。

环境变量组至少提供：

| 变量 | 类型 / 说明 |
|---|---|
| `DEPLOY_ENV` | `development`、`testing` 或 `production` |
| `EAGLE_DATABASE_DRIVER` | `postgres`（默认）或 `mysql` |
| `EAGLE_DATABASE_DSN` | 私密，当前环境 RDS DSN |
| `EAGLE_AUTH_ISSUER` | 当前环境 JWT issuer，必填 |
| `EAGLE_AUTH_AUDIENCE` | 必填，当前环境独立的 JWT audience |
| `EAGLE_AUTH_SIGNING_KEY_HOST_DIRECTORY` | ECS 密钥目录的绝对路径 |
| `EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID` | `<kid>.pem` 的 kid，不含扩展名 |
| `EAGLE_BIND_ADDRESS` | 默认 `127.0.0.1`，由同机网关反代 |
| `EAGLE_HTTP_PORT` / `EAGLE_METRICS_PORT` | 默认 `8000` / `9101` |
| `EAGLE_DEPLOY_TIMEOUT` | 应用健康检查等待秒数，默认 90 |
| `EAGLE_DEPLOY_ROOT` | 默认 `/opt/eagle` |

Google/Apple 登录、token TTL、日志与追踪等默认值见 `configs/config.yaml`，部署变量示例见 `deploy/environments/*.env.example`
及 `deploy/scripts/deploy.sh`。变量值不接受原始单引号、CR 或换行；PostgreSQL URL 中的
用户名/密码按 URL 规则编码，MySQL 使用其驱动 DSN 语法。不要对整条 DSN 进行 URL 编码。
ACR 登录可使用主机凭据助手，或注入上一节的三个登录变量。

RDS 仅允许对应 ECS 的私网访问；PostgreSQL 使用 `sslmode=require` 或更强，MySQL 启用 TLS。
本地 `deploy/docker-compose.yml` 内置数据库只用于开发。入口网关提供 TLS，metrics 不对公网开放。

## 5. CD 主机部署任务

开发 CD 选择 CI 的 Flow 流水线制品源，成功后触发；测试 CD 人工选择**开发环境已验证的同一制品版本**，
生产 CD 同样人工提升测试环境已验证的制品版本，不要重新构建或盲选另一次 `lastSuccessfulBuild`。配置方法见
[流水线源](https://help.aliyun.com/zh/yunxiao/user-guide/pipeline-sources)。

使用 Flow **主机部署**任务，勾选下载制品，指定 `eagle-release` 制品和当前环境主机组。
例如把下载文件路径配置为 `/tmp/eagle-testing/eagle-release.tgz`，部署命令填写：

```bash
set -eu
release_dir=$(mktemp -d /tmp/eagle-release.XXXXXXXX)
trap 'rm -rf "$release_dir"' EXIT
tar -xzf /tmp/eagle-testing/eagle-release.tgz -C "$release_dir"
sh "$release_dir/deploy/scripts/cd.sh"
```

按云效实际下载后的文件路径调整 tar 参数；如上传步骤额外包了一层归档，先解开外层，找到
`eagle-release.tgz`。不要使用构建机的 `PROJECT_DIR`/`CI_WORKSPACE` 定位 ECS 文件。
制品下载路径按环境/流水线隔离，并在 Flow 禁止同一环境流水线并发运行，避免尚未进入脚本文件锁时
两个任务覆盖下载包。部署机只读取发布包及变量，不需要安装 Git、Go 或 Python。
主机任务和制品下载机制见[官方主机部署文档](https://help.aliyun.com/zh/yunxiao/user-guide/host-deployment-1)。

部署顺序：

1. 校验环境、镜像 digest、密钥文件，取得环境文件锁。
2. 创建新快照，保存固定镜像地址、Compose 插值变量和 `0600` 的私密运行变量。
3. 校验 Compose，拉取固定 digest，以同一镜像执行迁移。
4. 替换应用，通过容器 `/app/healthcheck` 检查 metrics 端口的 `/readyz`。
5. 健康后原子更新 `current` 指针，保留 `previous`；任一步失败让流水线失败。

拉取/迁移失败不替换旧服务。应用启动失败或部署收到 INT/TERM 后会尝试恢复之前的完整快照；
首次发布失败则移除失败的应用容器。回滚失败明确报错，需人工处理。主机宕机或 SIGKILL 无法执行退出处理，
恢复后按快照检查实际容器状态再发布。部署完成后可从入口网关执行业务 API 冒烟测试；
应用健康检查不覆盖网关、DNS 和外部第三方登录连通性。

## 6. 回滚与运行文件

目录示例：

```text
/opt/eagle/testing/
├── deploy.lock
├── current                       # 当前成功快照的目录名，普通文本文件
├── previous                      # 上一成功快照的目录名
└── releases/release.XXXXXXXX/
    ├── compose.yml
    ├── compose.env                # 镜像、主机端口、密钥挂载目录
    └── runtime.env                # DSN 等运行配置，0600
```

回滚任务使用已验证发布包里的脚本，在同一环境运行：

```bash
DEPLOY_ENV=testing sh deploy/scripts/cd.sh rollback
```

回滚复用主机快照和本地旧镜像，不要求重新输入 DSN 或镜像地址，不执行数据库迁移。
若使用自定义根目录，仍须传入相同 `EAGLE_DEPLOY_ROOT`。成功回滚后 current / previous 交换，
再次 rollback 可切回刚才的版本。不要清理这两个快照引用的镜像或密钥。

旧版 `/opt/eagle/<environment>/runtime.env` + `compose.yml` 会在首次执行时转存为初始快照，
便于第一次升级失败时恢复。确认新流程稳定后可手动移除旧平铺文件；后续脚本以指针为准。
历史/失败快照保留用于定位问题，由运维定期清理非 current/previous 的目录；不要上传其中的私密配置。

**数据库不自动回退。** 迁移必须采用 expand/contract，旧字段保留到旧版本退出回滚窗口以后再清理。
上线前启用 RDS 备份与恢复点，迁移评审要求见[迁移兼容性](migration-compatibility.md)。
