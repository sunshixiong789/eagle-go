package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SocialIdentity is an Eagle identity backed by one social provider subject.
type SocialIdentity struct{ ent.Schema }

func (SocialIdentity) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "social_identity"}}
}

func (SocialIdentity) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("subject").MaxLen(384).Unique(),
		field.String("provider").MaxLen(16),
		field.String("provider_subject").MaxLen(255),
		field.String("email").MaxLen(320).Default(""),
		field.Bool("email_verified").Default(false),
		field.String("display_name").MaxLen(128).Default(""),
		field.String("avatar_url").MaxLen(2048).Default(""),
		field.String("role").MaxLen(32).Default("user"),
		field.Time("last_login_at"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SocialIdentity) Indexes() []ent.Index {
	return []ent.Index{index.Fields("provider", "provider_subject").Unique()}
}
