package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/schema/mixins"
)

// AICaptureSessionItem records durable, idempotent submission progress.
type AICaptureSessionItem struct {
	ent.Schema
}

func (AICaptureSessionItem) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.BaseMixin{}}
}

func (AICaptureSessionItem) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("session_id", uuid.UUID{}),
		field.String("client_id").MaxLen(255),
		field.UUID("entity_id", uuid.UUID{}).Optional().Nillable(),
		field.Enum("status").Values("pending", "creating", "attaching", "completed", "failed").Default("pending"),
		field.Text("uploaded_photo_ids").Default("[]"),
		field.String("error_code").MaxLen(64).Optional(),
	}
}

func (AICaptureSessionItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", AICaptureSession.Type).
			Ref("items").
			Field("session_id").
			Unique().
			Required(),
	}
}

func (AICaptureSessionItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "client_id").Unique(),
	}
}
