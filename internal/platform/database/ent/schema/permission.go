package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Permission 是前端导航节点，permission_code 引用后端 permission_definition 目录。
// parent_id 在数据库中以 NULL 表示根节点，领域层映射为 0；外键由 goose 迁移维护。
type Permission struct {
	ent.Schema
}

// Annotations 指定表名。
func (Permission) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "navigation_node"},
	}
}

// Fields 定义字段。
//
// type / status 用数值而非 ent enum：proto 契约里是 int32，
// 字典表种入的也是 "1"/"2"/"3"，换成字符串枚举会让三处表示不一致。
func (Permission) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),

		field.Int64("parent_id").
			Optional().
			Nillable().
			Comment("顶级节点在数据库中为 NULL，领域层映射为 0"),

		field.String("name").
			MaxRuneLen(64).
			NotEmpty(),

		field.String("code").
			MaxLen(128).
			Optional().
			Nillable().
			StorageKey("permission_code").
			Comment("权限码，如 system:dict:add。目录/菜单可为空，按钮必填"),

		field.Int32("type").
			Comment("1=目录 2=菜单 3=按钮"),

		field.String("path").MaxRuneLen(255).Default(""),
		field.String("component").MaxRuneLen(255).Default(""),
		field.String("icon").MaxRuneLen(64).Default(""),

		field.Int32("sort").Default(0),
		field.Bool("visible").Default(true),

		field.Int32("status").
			Default(1).
			Comment("0=禁用 1=正常"),

		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes 定义索引。
func (Permission) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code").Unique(),
		index.Fields("parent_id"),
	}
}
