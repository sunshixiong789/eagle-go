module github.com/eagle-go/eagle/tools

go 1.27.0

// 迁移程序的运行时依赖：连接 PostgreSQL/MySQL 并执行 goose SQL 迁移。
require (
	github.com/go-sql-driver/mysql v1.10.0 // MySQL 驱动
	github.com/jackc/pgx/v5 v5.11.0 // PostgreSQL 驱动
	github.com/pressly/goose/v3 v3.28.0 // SQL 迁移引擎
)

tool (
	entgo.io/ent/cmd/ent // 数据模型代码生成
	github.com/bufbuild/buf/cmd/buf // Protobuf 校验、兼容性检查与生成编排
	github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v3 // Kratos HTTP 传输层生成
	github.com/golangci/golangci-lint/v2/cmd/golangci-lint // Go 静态检查聚合器
	github.com/google/gnostic/cmd/protoc-gen-openapi // OpenAPI 文档生成
	github.com/pressly/goose/v3/cmd/goose // SQL 迁移命令行工具
	google.golang.org/protobuf/cmd/protoc-gen-go // Protobuf Go 类型生成
)
