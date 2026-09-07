package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AuthSession stores only a hash of the rotating refresh token.
type AuthSession struct{ ent.Schema }

func (AuthSession) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "auth_session"}}
}

func (AuthSession) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(32),
		field.Int64("identity_id"),
		field.String("refresh_token_hash").MaxLen(64).Unique(),
		field.Time("expires_at"),
		field.Time("revoked_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (AuthSession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("identity_id"),
		index.Fields("expires_at"),
	}
}
