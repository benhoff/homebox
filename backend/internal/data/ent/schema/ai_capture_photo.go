package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/schema/mixins"
)

// AICapturePhoto stores metadata for a temporary capture-session blob.
type AICapturePhoto struct {
	ent.Schema
}

func (AICapturePhoto) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.BaseMixin{}}
}

func (AICapturePhoto) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("session_id", uuid.UUID{}),
		field.UUID("client_photo_id", uuid.UUID{}),
		field.Int("position").NonNegative(),
		field.UUID("capture_group_id", uuid.UUID{}).Optional().Nillable(),
		field.String("original_name").MaxLen(255),
		field.String("path"),
		field.String("mime_type").MaxLen(100),
		field.Int64("size_bytes").NonNegative(),
		field.String("content_hash").MaxLen(128),
	}
}

func (AICapturePhoto) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", AICaptureSession.Type).
			Ref("photos").
			Field("session_id").
			Unique().
			Required(),
	}
}

func (AICapturePhoto) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "client_photo_id").Unique(),
		index.Fields("session_id", "position").Unique(),
	}
}
