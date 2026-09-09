package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// UserAccount is the provider-independent Eagle account. Its string ID is the
// stable JWT subject and never exposes a database sequence or provider subject.
type UserAccount struct{ ent.Schema }

func (UserAccount) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_account"}}
}

func (UserAccount) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("subject").MaxLen(32),
		field.String("display_name").MaxRuneLen(128).Default(""),
		field.String("avatar_url").MaxRuneLen(2048).Default(""),
		field.Int32("status").Default(1),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
