# 测试

| 位置 | 测什么 | 依赖 |
|---|---|---|
| `internal/modules/<module>/domain/` | 不变量、权限码、防环、覆盖关系 | 无 |
| `pkg/authz/` | Casbin 与 `PermissionCode.Covers` 对齐、中间件 | 内存 Casbin |
| `internal/modules/<module>/infrastructure/` | 真实 SQL、事务、乐观锁、存储适配 | embedded-postgres / 临时目录 |
| `internal/platform/server/` | 错误映射和服务器装配 | 无 |
| `tests/architecture/` | 层依赖、禁依赖 | `go list` |
| `tests/e2e/` | 401 / 403 / 200 | 进程内 HTTP + 自签 JWT |

- 单测/集成测试不启 Docker。
- e2e 必须走真实验签。禁止塞假 `Principal` 绕过 `authn`。
- 改 `PermissionCode.Covers` 或 Casbin matcher 必须跑两边对照测试（`TestDomainCoversMatchesCasbinEnforcement`）。
- 架构测试必须保持绿色。
- 声称测过必须有命令输出。至少：`gofmt` 所改文件，以及相关包的 `go test`。
