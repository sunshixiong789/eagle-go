package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PolicyOutbox 保存与策略事务一同提交、等待发布的变更事件。
type PolicyOutbox struct{ ent.Schema }

func (PolicyOutbox) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "authz_policy_outbox"}}
}

func (PolicyOutbox) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("policy_version").NonNegative(),
		field.String("event_type").MaxLen(64),
		field.JSON("payload", map[string]any{}),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("published_at").Optional().Nillable(),
		field.Int32("attempts").Default(0).NonNegative(),
		field.String("last_error").Default(""),
	}
}

func (PolicyOutbox) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("published_at", "id"),
		index.Fields("policy_version").Unique(),
	}
}
