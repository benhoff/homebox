package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/schema/mixins"
)

// AICaptureSession stores a durable photo-capture and review workflow.
type AICaptureSession struct {
	ent.Schema
}

func (AICaptureSession) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.BaseMixin{},
		GroupMixin{ref: "ai_capture_sessions", field: "group_id"},
		UserMixin{ref: "ai_capture_sessions", field: "user_id"},
	}
}

func (AICaptureSession) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("location_id", uuid.UUID{}).Optional().Nillable(),
		field.String("location_name_snapshot").MaxLen(255),
		field.Enum("status").
			Values("capturing", "queued", "analyzing", "analysis_failed", "ready_for_review", "submitting", "completed").
			Default("capturing"),
		field.Text("draft_json").Optional(),
		field.Int("draft_revision").Default(0),
		field.Int("capture_revision").Default(0),
		field.Int("analysis_attempts").Default(0),
		field.Time("analysis_next_attempt_at").Optional().Nillable(),
		field.Int("photo_count").Default(0),
		field.Time("worker_lease_until").Optional().Nillable(),
		field.Text("reanalysis_json").Optional(),
		field.String("reanalysis_status").MaxLen(32).Optional(),
		field.Time("reanalysis_next_attempt_at").Optional().Nillable(),
		field.Time("reanalysis_worker_lease_until").Optional().Nillable(),
		field.String("error_code").MaxLen(64).Optional(),
		field.String("error_message").MaxLen(1000).Optional(),
		field.Time("finished_at").Optional().Nillable(),
		field.Time("analyzed_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("expires_at").Default(func() time.Time { return time.Now().Add(30 * 24 * time.Hour) }),
	}
}

func (AICaptureSession) Edges() []ent.Edge {
	owned := func(name string, target any) ent.Edge {
		return edge.To(name, target).Annotations(entsql.Annotation{OnDelete: entsql.Cascade})
	}

	return []ent.Edge{
		edge.From("location", Entity.Type).
			Ref("ai_capture_sessions").
			Field("location_id").
			Unique().
			Annotations(entsql.Annotation{OnDelete: entsql.SetNull}),
		owned("photos", AICapturePhoto.Type),
		owned("items", AICaptureSessionItem.Type),
	}
}

func (AICaptureSession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "updated_at"),
		index.Fields("group_id", "user_id", "status"),
		index.Fields("status", "worker_lease_until"),
		index.Fields("status", "analysis_next_attempt_at"),
		index.Fields("reanalysis_status", "reanalysis_next_attempt_at"),
	}
}
