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

// SubscriptionPurchase is the immutable entitlement snapshot created when a
// subscription plan is purchased or assigned.
type SubscriptionPurchase struct {
	ent.Schema
}

func (SubscriptionPurchase) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "subscription_purchases"}}
}

func (SubscriptionPurchase) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Int64("plan_id").Optional().Nillable(),
		field.String("name").MaxLen(100).Default(""),
		field.String("tier_code").MaxLen(20).Default("standard"),
		field.Float("price").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.String("currency").MaxLen(3).Default(""),
		field.Time("starts_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("status").MaxLen(20).Default("active"),
		field.Int("concurrency_entitlement").Default(0),
		field.Float("lifetime_quota_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Float("daily_quota_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Float("weekly_quota_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Float("monthly_quota_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Float("lifetime_usage_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Float("daily_usage_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Float("weekly_usage_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Float("monthly_usage_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Time("daily_window_start").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("weekly_window_start").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("monthly_window_start").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Bool("balance_topup_enabled").Default(false),
		field.String("source").MaxLen(30).Default("payment"),
		field.Int64("source_id").Optional().Nillable(),
		field.JSON("snapshot", map[string]any{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("notes").SchemaType(map[string]string{dialect.Postgres: "text"}).Default(""),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (SubscriptionPurchase) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("groups", SubscriptionPurchaseGroup.Type),
	}
}

func (SubscriptionPurchase) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "status", "expires_at"),
		index.Fields("plan_id"),
		index.Fields("source", "source_id").Unique(),
	}
}
