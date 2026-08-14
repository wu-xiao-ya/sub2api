package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GroupPromotion is a time-bounded billing discount for one API key group.
// Usage logs save a snapshot of an applied promotion, so deleting a promotion
// never changes the meaning of historical bills.
type GroupPromotion struct {
	ent.Schema
}

func (GroupPromotion) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "group_promotions"},
	}
}

func (GroupPromotion) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(200).NotEmpty(),
		field.String("description").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int64("group_id"),
		field.String("mode").MaxLen(32).NotEmpty().Comment("discount_factor or fixed_multiplier"),
		field.Float("value").SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}).Comment("discount factor or maximum multiplier"),
		field.Time("starts_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("ends_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Bool("enabled").Default(true),
		field.Int64("created_by").Optional().Nillable(),
		field.Int64("updated_by").Optional().Nillable(),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (GroupPromotion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("group_id", "enabled", "starts_at", "ends_at"),
		index.Fields("starts_at"),
		index.Fields("ends_at"),
		index.Fields("created_at"),
	}
}
