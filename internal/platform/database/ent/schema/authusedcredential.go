package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AuthUsedCredential 记录已成功换取会话的第三方身份凭证哈希。
// 同一哈希只能成功一次，避免捕获的登录请求在凭证过期前再开一套长期会话。
type AuthUsedCredential struct{ ent.Schema }

func (AuthUsedCredential) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "auth_used_credential"}}
}

func (AuthUsedCredential) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("credential_hash").MaxLen(64).Immutable(),
		field.Time("expires_at"),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (AuthUsedCredential) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at"),
	}
}
