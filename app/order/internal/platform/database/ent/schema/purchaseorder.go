package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type PurchaseOrder struct{ ent.Schema }

func (PurchaseOrder) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "purchase_order"}}
}

func (PurchaseOrder) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(36).Unique().Immutable(),
		field.String("owner_subject").MaxLen(128),
		field.String("status").MaxLen(32),
		field.Int64("total_cents").Positive(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (PurchaseOrder) Indexes() []ent.Index {
	return []ent.Index{index.Fields("owner_subject", "created_at").StorageKey("idx_purchase_order_owner_created")}
}
