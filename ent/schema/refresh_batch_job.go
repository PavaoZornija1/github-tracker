package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// RefreshJobStatus is the lifecycle of one repo refresh within a batch.
type RefreshJobStatus string

const (
	RefreshJobStatusPending   RefreshJobStatus = "pending"
	RefreshJobStatusSucceeded RefreshJobStatus = "succeeded"
	RefreshJobStatusFailed    RefreshJobStatus = "failed"
)

// Values returns allowed enum values for Ent.
func (RefreshJobStatus) Values() []string {
	return []string{
		string(RefreshJobStatusPending),
		string(RefreshJobStatusSucceeded),
		string(RefreshJobStatusFailed),
	}
}

// RefreshBatchJob is one idempotent refresh unit inside a batch.
type RefreshBatchJob struct {
	ent.Schema
}

// Fields of the RefreshBatchJob.
func (RefreshBatchJob) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("batch_id", uuid.UUID{}),
		field.UUID("repo_id", uuid.UUID{}),
		field.Enum("status").
			GoType(RefreshJobStatus("")).
			Default(string(RefreshJobStatusPending)),
		field.Int("attempt").
			NonNegative().
			Default(0),
		field.String("error_reason").
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the RefreshBatchJob.
func (RefreshBatchJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("batch", RefreshBatch.Type).
			Ref("jobs").
			Field("batch_id").
			Required().
			Unique(),
		edge.From("repository", Repository.Type).
			Ref("refresh_jobs").
			Field("repo_id").
			Required().
			Unique(),
	}
}

// Indexes of the RefreshBatchJob.
func (RefreshBatchJob) Indexes() []ent.Index {
	return []ent.Index{
		// One job per repo per batch — idempotency key for enqueue + processing.
		index.Fields("batch_id", "repo_id").
			Unique(),
		index.Fields("batch_id", "status"),
		index.Fields("status"),
	}
}
