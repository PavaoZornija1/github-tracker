package queue

import (
	"encoding/json"

	"github.com/google/uuid"
)

// RoutingKeyRefresh is the direct-exchange routing key for refresh jobs.
const RoutingKeyRefresh = "refresh"

// HeaderAttempt tracks how many times a message has been attempted (1-based after first delivery).
const HeaderAttempt = "x-attempt"

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
