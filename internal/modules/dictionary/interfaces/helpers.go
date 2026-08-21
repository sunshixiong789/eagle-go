// Package interfaces adapts dictionary use cases to protobuf transports.
package interfaces

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/eagle-go/eagle/internal/modules/dictionary/domain"
)

const (
	defaultPageSize int32 = 20
	maxPageSize     int32 = 200
)

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

func toStatus(v int32) domain.Status   { return domain.Status(v) }
func fromStatus(s domain.Status) int32 { return int32(s) }
func toStatusPtr(v *int32) *domain.Status {
	if v == nil {
		return nil
	}
	s := domain.Status(*v)
	return &s
}

func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
