package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// RefreshBatch groups async refresh jobs started by refresh-all.
type RefreshBatch struct {
	ent.Schema
}

// Fields of the RefreshBatch.
func (RefreshBatch) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the RefreshBatch.
func (RefreshBatch) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("jobs", RefreshBatchJob.Type),
	}
}
