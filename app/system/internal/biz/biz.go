// Package biz 是 system 服务的领域层：用例编排、领域模型、仓储接口。
//
// 本包不 import 任何 pgx / redis / protobuf——依赖方向由内向外倒置，
// 领域规则因此可以脱离数据库和传输层单独做单元测试。
package biz

import "github.com/google/wire"

// ProviderSet 是 biz 层的 wire provider 集合。
var ProviderSet = wire.NewSet(
	NewUserUsecase,
	NewRoleUsecase,
	NewPermissionUsecase,
	NewDictUsecase,
)
