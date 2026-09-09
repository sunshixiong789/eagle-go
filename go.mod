module github.com/eagle-go/eagle

go 1.27.0

// 直接依赖按用途标记为：API、认证、授权、数据、可观测性和测试。
require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.12-20260825204119-511051f7f437.2 // API：Protovalidate 规则的生成类型
	entgo.io/ent v0.14.6 // 数据：数据模型与类型安全查询
	github.com/casbin/casbin/v2 v2.135.0 // 授权：基于策略的访问控制
	github.com/coreos/go-oidc/v3 v3.21.0 // 认证：Google/Apple OIDC 身份校验
	github.com/fergusstrange/embedded-postgres v1.34.0 // 测试：启动隔离的真实 PostgreSQL
	github.com/go-jose/go-jose/v4 v4.1.5 // 认证：JOSE、JWK 与 JWT 加解签
	github.com/go-kratos/kratos/contrib/middleware/validate/v3 v3.0.0-20260626125723-668db92c2c00 // API：Protobuf 请求校验中间件
	github.com/go-kratos/kratos/contrib/otel/v3 v3.0.0-20260626125723-668db92c2c00 // 可观测性：Kratos 的 OpenTelemetry 集成
	github.com/go-kratos/kratos/v3 v3.0.0 // API：服务框架与 HTTP 传输层
	github.com/go-sql-driver/mysql v1.10.0 // 数据：MySQL database/sql 驱动
	github.com/jackc/pgx/v5 v5.11.0 // 数据：PostgreSQL 驱动与连接池
	github.com/pressly/goose/v3 v3.28.0 // 数据：版本化 SQL 迁移
	github.com/prometheus/client_golang v1.24.1 // 可观测性：Prometheus 指标端点
	go.opentelemetry.io/otel v1.46.0 // 可观测性：OpenTelemetry 核心 API
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.46.0 // 可观测性：通过 OTLP/gRPC 导出链路
	go.opentelemetry.io/otel/exporters/prometheus v0.68.0 // 可观测性：导出 Prometheus 指标
	go.opentelemetry.io/otel/metric v1.46.0 // 可观测性：指标 API
	go.opentelemetry.io/otel/sdk v1.46.0 // 可观测性：链路 SDK
	go.opentelemetry.io/otel/sdk/metric v1.46.0 // 可观测性：指标 SDK
	go.opentelemetry.io/otel/trace v1.46.0 // 可观测性：链路 API
	google.golang.org/genproto/googleapis/api v0.0.0-20260904194346-d0f1323225a4 // API：Google API HTTP 注解类型
	google.golang.org/protobuf v1.36.12 // API：Protobuf Go 运行时
)
