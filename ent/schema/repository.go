package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Repository holds a tracked GitHub repository.
type Repository struct {
	ent.Schema
}

// Fields of the Repository.
func (Repository) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("owner").
			NotEmpty(),
		field.String("name").
			NotEmpty(),
		field.String("full_name").
			NotEmpty().
			Unique(),
		field.String("description").
			Optional().
			Nillable(),
		field.Int("stars").
			NonNegative().
			Default(0),
		field.String("language").
			Optional().
			Nillable(),
		field.String("html_url").
			NotEmpty(),
		field.String("notes").
			Default(""),
		field.Time("fetched_at").
			Default(time.Now),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the Repository.
func (Repository) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("refresh_jobs", RefreshBatchJob.Type),
	}
}

// Indexes of the Repository.
func (Repository) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("language"),
		index.Fields("stars", "id"),
		index.Fields("updated_at", "id"),
	}
}
