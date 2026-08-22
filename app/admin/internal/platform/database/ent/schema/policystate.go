package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// PolicyState 保存授权策略的单调递增版本。
//
// 表中固定只有 id=1 一行。Redis 通知只是策略同步的快速路径，各副本
// 通过这个版本号定期对账，从而补回订阅断线期间丢失的通知。
type PolicyState struct{ ent.Schema }

func (PolicyState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "authz_policy_state"}}
}

func (PolicyState) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("version").Default(0).NonNegative(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
