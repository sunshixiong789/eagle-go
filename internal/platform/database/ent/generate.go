// Package ent contains the generated persistence client.
//
// 这里刻意不放 //go:generate：ent 代码生成器由 tools/go.mod 锁版本，
// `go run entgo.io/ent/cmd/ent` 会在业务 module 里解析一遍它的依赖。
// 统一走 `make ent`（Makefile 里从 tools module 编译出 bin/ent 再执行）。
package ent
