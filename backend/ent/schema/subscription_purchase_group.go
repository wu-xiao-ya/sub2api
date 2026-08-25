package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SubscriptionPurchaseGroup stores the group authorization snapshot for one
// purchase. It intentionally does not follow later plan edits.
type SubscriptionPurchaseGroup struct {
	ent.Schema
}

func (SubscriptionPurchaseGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "subscription_purchase_groups"}}
}

func (SubscriptionPurchaseGroup) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("purchase_id"),
		field.Int64("group_id"),
		field.String("group_name").MaxLen(100).Default(""),
		field.String("platform").MaxLen(50).Default(""),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (SubscriptionPurchaseGroup) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("purchase", SubscriptionPurchase.Type).
			Ref("groups").
			Field("purchase_id").
			Unique().
			Required(),
	}
}

func (SubscriptionPurchaseGroup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("purchase_id", "group_id").Unique(),
		index.Fields("group_id"),
	}
}
