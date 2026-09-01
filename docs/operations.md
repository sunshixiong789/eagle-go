# 生产运行手册

本文只说明生产运行和故障处置；本地启动见[开发环境部署](development-deployment.md)，
模块边界见[架构说明](architecture.md)，发布顺序见[生产环境部署](deployment.md)。

## 发布与回滚

推送 `v*` tag 或手动运行 `Release images` workflow。流水线先编译测试并校验部署文件，
再构建 amd64/arm64 镜像，生成 SBOM 和 provenance、执行 HIGH/CRITICAL 漏洞扫描，
最后输出不可变 digest。

镜像发布与环境部署刻意分离。生产按
「migration 任务 → 服务滚动 → 指标观察」推进，顺序不能颠倒：进程启动时会校验 proto
声明的权限码与数据库 catalog 一致，迁移没跑完就发服务会让新实例全部 fail closed。

回滚只回退镜像 digest。已经执行的数据库变更必须保持向后兼容，不随进程回滚自动撤销；
需要撤销 schema 时，走一次正向的 contract 发布，而不是回滚迁移。

上线后至少观察 30 分钟：可用性错误预算、p99、实例重启、数据库连接池、文件清理告警。
fast burn 告警触发时停止发布；不要在错误预算耗尽时继续常规变更。

## 权限变更的生效延迟

权限策略存在数据库里，每个实例本地持有一份 Casbin 模型，靠版本号每 5 秒对账一次。

这意味着改完角色绑定后，最坏情况下要等一个对账周期才会在所有实例生效。排查
「权限已经改了但还是 403」时，先确认是否只是没到对账周期，再去查策略本身。

`/readyz` 上的 `authz-policy` 检查会查一次数据库里的策略版本：数据库不可达时 readiness
失败、实例被摘流量；数据库可达但本地策略落后时，readiness 仍然通过，只把版本差记为指标。
所以「策略陈旧」要看指标，不要指望 readiness 报警。

## 文件清理

清理 worker 每 10 分钟扫一次，回收超过 1 小时仍处于 `PENDING` / `DELETING` 的记录：
先删对象、再删元数据。`READY` 的文件不在范围内。

每个实例都跑，不选主。多实例同时选中同一批记录是可以接受的——两步删除都对
「已经不存在」免疫，重复执行只是白跑。

日志里持续出现 `cleanup stale files failed` 通常指向对象存储不可达或凭据失效，
而不是文件本身的问题；此时元数据会一直堆在 `PENDING`，恢复存储后自然收敛。

## 可观测性与 SLO

本地 `--profile obs` 会启动 Prometheus、Alertmanager、Tempo、Loki、Alloy 和 Grafana，
并自动加载 `Eagle production overview`。Prometheus 抓 `eagle:9100` 和 `minio:9000`，
告警规则在 [`deploy/observability/rules/eagle-slo.yml`](../deploy/observability/rules/eagle-slo.yml)。

已定义的告警：

| 告警 | 含义 |
|---|---|
| `EagleServiceUnavailable` | 抓取目标 down |
| `EagleAvailabilityErrorBudgetFastBurn` | 错误预算快速消耗，需立即处置 |
| `EagleAvailabilityErrorBudgetSlowBurn` | 错误预算缓慢消耗，需排期处理 |
| `EagleP99LatencyHigh` | p99 超过 2s |

这套规则是本地可跑的基线，不是生产容量结论。生产 SLO 目标为 30 天 99.9% HTTP 可用性，
阈值必须按真实流量和压测数据校准，并补上数据库连接池、对象存储和备份失败的监控。

告警必须进入有人值守的通知渠道，并按季度做一次「触发 → 通知 → 确认 → 恢复」的演练——
没演练过的告警链路，在真正出事时大概率不通。日志只写结构化 stdout 由平台收集；
日志、指标、trace 使用同一 `service_name`、trace ID 和发布时间标签关联。

## 备份、恢复与容灾

只有一个数据库，备份策略也只有一份。优先使用托管 PostgreSQL 的 PITR、跨可用区高可用和
跨区域副本；自建时至少保证 `pg_dump -Fc` 定期执行、用 `pg_restore --list` 校验完整性、
以服务端加密上传到对象存储，并在 bucket 上配置版本控制、不可变保留和异地复制。

备份任务不应持有删除权限，过期清理交给存储的生命周期策略——运维脚本有 bug 时，
最坏结果应该是备份变多，不是备份消失。

对象存储中的文件是独立的一份数据。数据库备份不包含它们，恢复演练要同时覆盖两边，
否则会恢复出一堆指向不存在对象的元数据。

每月至少恢复一次到隔离数据库，执行迁移、核心读接口和数据量核对。生产建议目标：
数据库 RPO ≤ 15 分钟（PITR）、RTO ≤ 60 分钟；对象存储 RPO 由供应商复制策略定义。
每半年做一次区域级演练并记录实际 RTO。

## 常见故障定位

| 现象 | 先看 |
|---|---|
| 全部请求 401 | `EAGLE_AUTH_ISSUER` 与 token `iss` 是否逐字一致；JWKS 是否可达 |
| 部分请求 403 | 是否在策略对账周期内；角色是否落在正确的命名空间（`realm:` vs `client:`） |
| 新实例起不来 | 迁移是否已执行；权限码 catalog 校验失败会 fail closed |
| `/readyz` 不通但进程活着 | 数据库连接或策略加载失败，`/livez` 仍会返回 200 |
| 上传大文件失败且无应用日志 | 入口层请求体上限低于应用配置，请求在网关就被截断了 |

## 上线检查

- 所有镜像以 digest 部署，签名/provenance 校验通过；
- 迁移任务与服务使用同一 digest，且迁移先完成；
- Secret 来自受控系统并支持轮换，本地示例凭据未被带入；
- 数据库、OIDC、S3、OTLP 全部使用 TLS 地址和受信 CA；
- 备份已执行且做过一次真实恢复演练；
- 资源配额、扩缩容阈值和告警阈值经过容量测试校准；
- 发布负责人、回滚条件和观察窗口已经记录。
