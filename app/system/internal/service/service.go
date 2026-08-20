// Package service 是 system 服务的传输适配层。
//
// 职责只有两件：proto 消息与领域模型互转、把 context 里的调用者身份
// 传给用例。业务规则一律不写在这里——那是 biz 层的事。
package service

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/eagle-go/eagle/app/system/internal/domain"
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

// ── 领域值对象 ↔ proto 标量 ────────────────────────────────
//
// proto 用 int32 表达状态，领域层用 Status 值对象。转换收敛在这里，
// 避免类型转换散落进每个 handler。

func toStatus(v int32) domain.Status { return domain.Status(v) }

func fromStatus(s domain.Status) int32 { return int32(s) }

// toStatusPtr 转换可选的状态过滤条件，nil 表示不按状态过滤。
func toStatusPtr(v *int32) *domain.Status {
	if v == nil {
		return nil
	}
	s := domain.Status(*v)
	return &s
}

// toPermissionTypePtr 转换可选的节点类型过滤条件。
func toPermissionTypePtr(v *int32) *domain.PermissionType {
	if v == nil {
		return nil
	}
	t := domain.PermissionType(*v)
	return &t
}

// ts 把时间转成 protobuf 时间戳，零值时间返回 nil 而不是 1970-01-01。
func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
