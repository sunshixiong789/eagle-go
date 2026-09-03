package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// PolicyState 保存授权策略的单调递增版本。表中固定只有 id=1 一行，
// 各副本通过数据库版本周期对账，不依赖任何外部消息或缓存组件。
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
