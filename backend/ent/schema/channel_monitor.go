package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ChannelMonitor holds the schema definition for the ChannelMonitor entity.
// ???????????? provider/endpoint/api_key ??????????
type ChannelMonitor struct {
	ent.Schema
}

func (ChannelMonitor) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "channel_monitors"},
	}
}

func (ChannelMonitor) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (ChannelMonitor) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty().
			MaxLen(100),
		field.Enum("provider").
			Values("openai", "anthropic", "gemini", "grok", "antigravity", "deepseek", "kimi", "glm", "qwen", "minimax", "mimo", "hunyuan"),
		field.String("api_mode").
			Default("chat_completions").
			MaxLen(32).
			Comment("OpenAI request protocol: chat_completions or responses; non-OpenAI uses chat_completions"),
		field.String("endpoint").
			NotEmpty().
			MaxLen(500).
			Comment("Provider base origin, e.g. https://api.openai.com"),
		field.String("api_key_encrypted").
			NotEmpty().
			Sensitive().
			Comment("AES-256-GCM encrypted API key"),
		field.String("source_mode").
			Default("direct_upstream").
			MaxLen(24).
			Comment("direct_upstream or internal_gateway"),
		field.Int64("internal_api_key_id").
			Optional().
			Nillable().
			Comment("Station API key used by internal gateway monitors"),
		field.Int64("internal_group_id").
			Optional().
			Nillable().
			Comment("Station API key group snapshot"),
		field.String("primary_model").
			NotEmpty().
			MaxLen(200),
		field.JSON("extra_models", []string{}).
			Default([]string{}).
			Comment("Additional model names to test alongside primary_model"),
		field.String("group_name").
			Optional().
			Default("").
			MaxLen(100),
		// Explicitly binds a monitor to one account-management group. Legacy
		// monitors keep this null and continue using their static API key.
		field.Int64("account_group_id").
			Optional().
			Nillable(),
		field.Bool("enabled").
			Default(true),
		field.Int("interval_seconds").
			Range(15, 3600),
		field.Int("jitter_seconds").
			Default(0).
			Range(0, 3600).
			Comment("????? interval ??? ? [0, jitter] ???????????0 ???????service ???? interval - jitter >= 15"),
		field.Int("request_timeout_seconds").
			Default(45).
			Range(15, 900).
			Comment("?????????????????????????????"),
		field.Time("last_checked_at").
			Optional().
			Nillable(),
		field.Int64("created_by"),

		// ---- ?????????????? / ????? ----

		// template_id: ??????? ID???? UI ?? + ??????
		// ????? checker ???? 3 ??????**???????**?
		// ??????????? SET NULL?? Edges ? OnDelete ????
		field.Int64("template_id").
			Optional().
			Nillable(),
		// extra_headers: ??? HTTP ???????? or ??????
		// ??? merge ? adapter ?? headers?
		field.JSON("extra_headers", map[string]string{}).
			Default(map[string]string{}),
		// body_override_mode: ? ChannelMonitorRequestTemplate.body_override_mode
		field.String("body_override_mode").
			Default("off").
			MaxLen(10),
		// body_override: ? ChannelMonitorRequestTemplate.body_override
		field.JSON("body_override", map[string]any{}).
			Optional(),
	}
}

func (ChannelMonitor) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("history", ChannelMonitorHistory.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("daily_rollups", ChannelMonitorDailyRollup.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		// ????????????? template_id ?????
		// ?????????????????
		edge.To("request_template", ChannelMonitorRequestTemplate.Type).
			Field("template_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (ChannelMonitor) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("enabled", "last_checked_at"),
		index.Fields("provider"),
		index.Fields("provider", "api_mode"),
		index.Fields("group_name"),
		index.Fields("template_id"),
		index.Fields("source_mode", "internal_api_key_id"),
	}
}
