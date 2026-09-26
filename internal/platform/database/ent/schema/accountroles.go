package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AccountRoleState 串行化同一 audience 的角色管理与一次性管理员初始化。
type AccountRoleState struct{ ent.Schema }

func (AccountRoleState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "account_role_state"}}
}
func (AccountRoleState) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("audience").MaxLen(255), field.Int64("revision").Default(1),
		field.Bool("admin_initialized").Default(false), field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// UserAudience 标记账号已在某个 audience 初始化角色，保证显式空授权不被登录流程覆盖。
type UserAudience struct{ ent.Schema }

func (UserAudience) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_audience"}}
}
func (UserAudience) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"), field.String("account_subject").MaxLen(32), field.String("audience").MaxLen(255),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
func (UserAudience) Indexes() []ent.Index {
	return []ent.Index{index.Fields("account_subject", "audience").Unique()}
}

// AccountRoleAudit 保存角色变更的操作人、目标、版本和前后角色，和角色修改一同提交。
type AccountRoleAudit struct{ ent.Schema }

func (AccountRoleAudit) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "account_role_audit"}}
}
func (AccountRoleAudit) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"), field.String("audience").MaxLen(255), field.Int64("revision"),
		field.String("account_subject").MaxLen(32), field.String("actor_subject").MaxLen(128), field.String("action").MaxLen(32),
		field.JSON("before", []string{}), field.JSON("after", []string{}), field.Time("created_at").Default(time.Now).Immutable(),
	}
}
func (AccountRoleAudit) Indexes() []ent.Index {
	return []ent.Index{index.Fields("audience", "revision").Unique(), index.Fields("account_subject", "created_at")}
}
