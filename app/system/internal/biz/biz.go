// Package biz 编排跨聚合用例，不处理 HTTP/gRPC 错误，也不依赖 ent / protobuf。
package biz

import "github.com/google/wire"

// ProviderSet 是 biz 层的 wire provider 集合。
var ProviderSet = wire.NewSet(
	NewPermissionUsecase,
	NewDictUsecase,
	NewRoleBindingUsecase,
)
