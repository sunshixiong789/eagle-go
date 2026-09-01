# 生产环境部署

本文描述生产制品、平台前置条件和安全发布顺序。本地 Compose、宿主机调试和 IDEA 入口见
[开发环境部署](development-deployment.md)，告警、备份恢复和故障处置见
[生产运行手册](operations.md)。

## 发布单元

只有一个：`eagle`。

一个 Go module、一个二进制、一个数据库、一个镜像。镜像里有三个可执行文件：

| 路径 | 角色 |
|---|---|
| `/app/eagle` | 常驻服务进程，默认 entrypoint |
| `/app/migrate` | 一次性迁移任务，发布前执行 |
| `/app/healthcheck` | 容器 HEALTHCHECK 探针 |

三者同镜像同版本，这样不会出现「服务已升级、迁移还没跑」的窗口。`migrations/` 和
`configs/` 也在镜像里，迁移文件与代码永远同版本。

仓库本身只提供 Docker Compose 作为部署资产。如果目标环境是 Kubernetes 或其它编排系统，
清单由环境仓库自行维护——本文描述的是发布契约（制品、顺序、前置条件），编排方式可以替换。

## 构建不可变镜像

正式发布通过 `Release images` GitHub Actions workflow 生成 amd64/arm64 镜像、SBOM、
provenance 和漏洞扫描结果。

发布 tag：

```bash
git tag v1.2.0
```

```bash
git push origin v1.2.0
```

也可以在发布平台手动触发 workflow 并提供唯一版本号。环境仓库必须使用 workflow 输出的
镜像 digest，例如 `ghcr.io/example/eagle@sha256:...`，不要使用 `stable`、`staging` 或
`latest` 等可变标签——可变标签会让「当前生产跑的是哪份代码」这个问题失去确定答案。

本地验证镜像：

```bash
make image VERSION=v1.2.0 REGISTRY=registry.example.com/eagle
```

## 平台前置条件

应用上线前，平台必须已经提供：

| 能力 | 要求 |
|---|---|
| 入口 | TLS 终止、固定域名、请求体上限、真实客户端 IP 透传 |
| 身份 | 生产 OIDC IdP，issuer 与 token 的 `iss` 逐字一致 |
| 数据 | PostgreSQL 17，最小权限账号，定期备份并验证过恢复 |
| 对象存储 | S3 兼容服务与独立凭据（`file` 模块使用） |
| 可观测性 | OTLP 接收端、指标抓取、日志与 trace 后端 |
| Secret | External Secrets、Vault 或云 Secret Manager |

仓库不提供生产级 PostgreSQL、对象存储或 IdP 的部署清单。Compose 里的 `postgres`、`minio`、
`keycloak` 只用于本地，单副本、无备份、凭据写死在文件里，不能带进生产。

## 配置与 Secret

配置全部通过 `EAGLE_` 前缀环境变量注入，启动时做一次校验，缺失或非法直接拒绝启动。

最少必须提供：

| 变量 | 说明 |
|---|---|
| `EAGLE_DATABASE_DSN` | 生产库 DSN，必须 `sslmode=require` 或更强 |
| `EAGLE_AUTH_ISSUER` | 与 token `iss` 逐字一致，含大小写和尾部斜杠 |
| `EAGLE_AUTH_CLIENT_ID` | 本服务在 IdP 中的 client |
| `EAGLE_AUTH_AUDIENCE` | 留空则不校验 aud；生产建议配上并在 IdP 加 audience mapper |
| `EAGLE_FILE_S3_*` | endpoint / bucket / access key / secret key |

换 IdP 时按需覆盖 `EAGLE_AUTH_JWKS_URL`、`EAGLE_AUTH_JWKS_PATH`、
`EAGLE_AUTH_REALM_ROLES_CLAIM`、`EAGLE_AUTH_CLIENT_ROLES_CLAIM`，**不需要改代码**。
各 IdP 的取值对照见 [`deploy/keycloak/README.md`](../deploy/keycloak/README.md)。

敏感值只能来自 Secret 管理系统，不写进镜像、ConfigMap 或 Compose 文件。上线前核对：

- token 的 `iss` 与 `EAGLE_AUTH_ISSUER` 完全一致（不一致的表现是全部请求 401，且日志里
  只有验签失败，很容易误判成 IdP 故障）；
- DSN 账号只能访问自己的 database，不具备 `SUPERUSER` 或跨库权限；
- OIDC、S3、OTLP 都走 TLS 地址和受信 CA；
- TLS 证书、数据库口令、S3 凭据支持独立轮换。

静态检查 Compose 文件能否解析：

```bash
make validate-deploy
```

该命令只证明文件语法正确，不证明生产依赖存在，也不执行迁移或发布。

## 迁移与滚动发布

服务进程启动时**不会**自动迁移。多副本同时启动会并发修改 schema，必须由发布流程串行编排。

发布状态机：

```text
确认不可变镜像 digest
  → 用同一 digest 运行一次性迁移任务（/app/migrate -dir /app/migrations）
  → 等待迁移退出码为 0
  → 把服务更新到同一 digest
  → 等待 rollout 完成和 /readyz 通过
  → 观察指标与日志
```

迁移任务必须满足：

- 与即将发布的服务使用**完全相同**的镜像 digest；
- 名称包含本次发布标识（例如 `eagle-migrate-2639fc4`），不复用固定名称的旧任务；
- 失败时阻止服务更新；
- 执行记录保留到观察期结束。

Compose 里 `eagle-migrate` 用 `service_completed_successfully` 表达这个顺序，`eagle` 只有在
迁移成功退出后才启动。生产编排系统需要用等价机制（Job + init 依赖、流水线串行步骤等）
自己保证同样的先后关系。

## 数据库变更策略

数据库迁移采用 expand/contract：

1. expand：先添加兼容的新表、列、索引或双写能力；
2. rollout：发布仍兼容旧 schema 的应用；
3. backfill：限速回填并核对数据；
4. contract：确认所有旧版本退出后，再删除旧结构。

应用回滚只回退镜像 digest；已经执行的 schema 变更不得依赖进程回滚自动撤销。破坏性的
contract 迁移必须作为独立发布，在备份、兼容窗口和回滚方案确认后执行。

CI 会对 `migrations/` 执行 `up -> down-to 0 -> up`，保证回滚脚本不是写完就没人跑过的死代码。

## 入口与就绪

入口层（生产可以是 nginx、云 LB 或网关控制器，Compose 用 nginx）负责 TLS 终止、请求体上限
和真实客户端 IP 透传。应用只有一个上游，路由无需按路径拆分，全部转发到 `eagle:8000`。

请求体上限要与 `EAGLE_FILE_*` 的上传限制对齐。两边不一致时，超限请求会在网关被截断，
应用侧看不到任何日志——这类问题只在上线后传大文件时才暴露。

`/livez` 只表示进程存活，`/readyz` 聚合初始化状态和数据库检查，两者都在 metrics 端口
（默认 `9100`）上。流量只发给 readiness 通过的实例，滚动发布保持 `maxUnavailable: 0`，
并等待 rollout 真正完成，而不是只等资源 apply 成功。

## 多副本注意事项

单体水平扩容是安全的，但有两处依赖对账机制，扩容前要知道它们存在：

- **权限策略**：在副本 A 上改的角色绑定不会立刻在副本 B 生效。策略版本号每 5 秒对账一次，
  变化时原子替换本地 Casbin 模型。权限变更的生效延迟上界是这个周期。
- **启动校验**：进程启动时校验 proto 里声明的权限码与数据库 catalog 一致，不一致直接
  fail closed。这意味着**必须先跑迁移再发服务**——顺序反了会让新副本全部起不来，
  这是有意为之，半份策略比没有策略更危险。

文件清理 worker 在每个副本上都会运行，不选主。多个副本可能选中同一批过期记录，但删除
对象和删除元数据都对「已经不存在」免疫，重复执行只是白跑一遍，不会报错也不会误删——
它只处理 `PENDING` / `DELETING` 且超过保留期的记录，`READY` 的文件不在范围内。

## 上线检查

- 镜像使用 digest，签名/provenance 校验通过；
- 迁移任务使用同一 digest，状态为成功；
- 服务已更新到同一 digest，rollout 和 readiness 通过；
- 证书、域名、CORS 和请求体上限正确；
- Secret 来自受控系统，本地示例凭据未被带入；
- 资源配额和扩缩容阈值已按实际容量校准，不是照抄默认值；
- 错误率、p99、实例重启、数据库连接池和文件清理进入观察面板；
- 发布负责人、回滚条件和观察窗口已经记录。
