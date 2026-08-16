# 测试

| 位置 | 测什么 | 依赖 |
|---|---|---|
| `domain/` | 不变量、权限码、防环、覆盖关系 | 无 |
| `pkg/authz/` | Casbin 与 `PermissionCode.Covers` 对齐、中间件 | 内存 Casbin |
| `data/` | 真实 SQL、事务、乐观锁、缓存 | embedded-postgres + miniredis |
| `service/` | 错误映射 | 无 |
| `architecture/` | 层依赖、禁依赖 | `go list` |
| `e2e/` | 401 / 403 / 200 | 进程内 HTTP + 自签 JWT |

- 单测/集成测试不启 Docker。
- e2e 必须走真实验签。禁止塞假 `Principal` 绕过 `authn`。
- 改 `PermissionCode.Covers` 或 Casbin matcher 必须跑两边对照测试（`TestDomainCoversMatchesCasbinEnforcement`）。
- 架构测试必须保持绿色。
- 声称测过必须有命令输出。至少：`gofmt` 所改文件，以及相关包的 `go test`。
