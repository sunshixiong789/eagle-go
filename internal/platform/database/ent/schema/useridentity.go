package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserIdentity links one external login identity to a provider-independent account.
type UserIdentity struct{ ent.Schema }

func (UserIdentity) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_identity"}}
}

func (UserIdentity) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("account_subject").MaxLen(32),
		field.String("provider").MaxLen(32),
		field.String("provider_subject").MaxLen(255),
		field.String("email").MaxLen(320).Default(""),
		field.Bool("email_verified").Default(false),
		field.Time("last_login_at"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (UserIdentity) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider", "provider_subject").Unique(),
		index.Fields("account_subject"),
	}
}
