// Package ent 是数据访问层的生成代码根目录。
//
// 运行 `go generate ./ent` 重新生成。schema 定义见 ent/schema。
package ent

// 特性开关：
//   sql/execquery —— 暴露 ExecContext/QueryContext，供需要 Raw SQL 的场景使用
//   sql/upsert    —— 生成 OnConflict，Casbin 策略写入靠它做幂等
//go:generate go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/execquery,sql/upsert ./schema
