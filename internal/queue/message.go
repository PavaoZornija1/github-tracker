package queue

import (
	"encoding/json"

	"github.com/google/uuid"
)

// Routing keys on the main exchange.
const (
	RoutingKeyRefresh   = "refresh"
	RoutingKeyBatchKick = "batch.kick"
	RoutingKeyRetry     = "refresh.retry"
)

// AMQP header names.
const (
	HeaderAttempt     = "x-attempt"
	HeaderMessageType = "x-message-type"
)

// Message type values for HeaderMessageType.
const (
	MessageTypeRefresh   = "refresh"
	MessageTypeBatchKick = "batch.kick"
)

// RefreshJob is the queue payload for one repo refresh within a batch.
type RefreshJob struct {
	JobID   uuid.UUID `json:"job_id"`
	BatchID uuid.UUID `json:"batch_id"`
	RepoID  uuid.UUID `json:"repo_id"`
}

func (j RefreshJob) Marshal() ([]byte, error) {
	return json.Marshal(j)
}

func UnmarshalRefreshJob(body []byte) (RefreshJob, error) {
	var j RefreshJob
	err := json.Unmarshal(body, &j)
	return j, err
}

// BatchKick asks the worker to fan out PublishRefresh for still-pending jobs.
type BatchKick struct {
	BatchID uuid.UUID `json:"batch_id"`
}

func (k BatchKick) Marshal() ([]byte, error) {
	return json.Marshal(k)
}

func UnmarshalBatchKick(body []byte) (BatchKick, error) {
	var k BatchKick
	err := json.Unmarshal(body, &k)
	return k, err
}
