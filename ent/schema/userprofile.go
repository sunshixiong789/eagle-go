package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserProfile 是用户的业务扩展字段。
//
// Keycloak 是用户的唯一来源：账号、口令、邮箱、角色全在那边，
// 本表只存 Keycloak 不该管的业务属性（部门、岗位等），用 token 的 sub 关联。
//
// 刻意不在这里冗余用户名/邮箱：一旦冗余就要处理与 Keycloak 的一致性同步，
// 而那正是选择「单一用户源」时想避开的问题。展示用的用户名直接从
// token claim 取，需要批量查询时走 Keycloak Admin API。
type UserProfile struct {
	ent.Schema
}

// Annotations 指定表名。
func (UserProfile) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sys_user_profile"},
	}
}

// Fields 定义字段。
func (UserProfile) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),

		field.String("subject").
			MaxLen(64).
			Unique().
			Immutable().
			Comment("Keycloak token 的 sub，用户在本系统的唯一锚点"),

		field.Int64("dept_id").Default(0),
		field.String("position").MaxLen(64).Default("").Comment("岗位"),
		field.String("remark").Default(""),

		field.Time("last_seen_at").
			Optional().
			Nillable().
			Comment("最近一次携带有效 token 访问的时间"),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes 定义索引。
func (UserProfile) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("dept_id"),
	}
}
