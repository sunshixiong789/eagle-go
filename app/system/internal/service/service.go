// Package service 是 system 服务的传输适配层。
//
// 职责只有两件：proto 消息与领域模型互转、把 context 里的调用者身份
// 传给用例。业务规则一律不写在这里——那是 biz 层的事。
package service

import (
	"time"

	"github.com/google/wire"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProviderSet 是 service 层的 wire provider 集合。
var ProviderSet = wire.NewSet(
	NewPermissionService,
	NewDictService,
	NewRoleBindingService,
)

// 分页参数的边界。
const (
	defaultPageSize int32 = 20
	maxPageSize     int32 = 200
)

// paginate 把 1 基的 page/page_size 归一化成 offset/limit。
// 越界值一律收敛到合法区间，而不是报错——列表接口对参数应当宽容。
func paginate(page, pageSize int32) (offset, limit int32) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	if page <= 0 {
		page = 1
	}
	return (page - 1) * pageSize, pageSize
}

// ts 把时间转成 protobuf 时间戳，零值时间返回 nil 而不是 1970-01-01。
func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// tsPtr 处理可空时间（如 last_login_at）。
func tsPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil || t.IsZero() {
		return nil
	}
	return timestamppb.New(*t)
}
