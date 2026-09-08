package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserRoleBinding assigns an account a role only for one token audience.
type UserRoleBinding struct{ ent.Schema }

func (UserRoleBinding) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_role_binding"}}
}

func (UserRoleBinding) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("account_subject").MaxLen(32),
		field.String("audience").MaxLen(255),
		field.String("role").MaxLen(64),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (UserRoleBinding) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("account_subject", "audience", "role").Unique(),
		index.Fields("account_subject", "audience"),
	}
}
