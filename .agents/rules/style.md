# Go 写法

本仓库已落地的约束。不复述 Effective Go（`context` 第一参数、`gofmt`、值/指针接收器等模型已会）。

- 标准库优先：`slices`、`maps`、`cmp`、`encoding/json`、`errors`、`fmt.Errorf("%w")`、`log/slog`。
- 禁止新增直接依赖：`samber/lo`、`lancet`、`jinzhu/copier`、`mapstructure`、`spf13/cast`、`pkg/errors`、`logrus`、`zap`、`gorm`、Kratos v2。由 `TestBannedDependencies` 检查直接依赖。
- 类型映射写本层小函数（`toProtoPermission` / `toDomainPermission`），不上通用 mapper。
- domain/application 只用标准库 `error`。Kratos 错误和 `ErrorReason` 只出现在 server/interfaces。判断用 `errors.Is` / `errors.As`。
- 值对象只用于「写错会静默失败」的概念（当前：`PermissionCode`）。角色用 `type Role string` + `NewRole`，不要再套 struct。
- 参数 ≥ 4 个且同类型易传错时用 `XxxParams`。
- 注释只写非显而易见的约束。禁止 `// ID 返回 ID`。`revive` 的 `exported` 已关，不要为过 lint 补空话。不留占位 TODO。
- 包名不要 `utils` / `common` / `helpers` / `models`。不要 `XxxDTO` / `XxxDO` 进 domain。
- 日志用 `log/slog`。不要抄网上 Kratos v2 的 `log.Helper`。
- 手改 `*.proto`、`ent/schema`、`db/migrations`。禁止手改 `*.pb.go` 与 `ent/` 生成文件。
- `goimports` 本模块前缀：`github.com/eagle-go/eagle`。
