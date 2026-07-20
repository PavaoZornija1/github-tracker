package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/PavaoZornija1/github-tracker/ent/schema"
	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/ent"
	"github.com/PavaoZornija1/github-tracker/internal/ent/refreshbatch"
	"github.com/PavaoZornija1/github-tracker/internal/ent/refreshbatchjob"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/metrics"
	"github.com/PavaoZornija1/github-tracker/internal/queue"
)

// JobPublisher enqueues refresh jobs and batch kicks.
type JobPublisher interface {
	PublishRefresh(ctx context.Context, job queue.RefreshJob) error
	PublishBatchKick(ctx context.Context, kick queue.BatchKick) error
}

// BatchService manages refresh batches and idempotent job processing.
type BatchService struct {
	client     *ent.Client
	repos      *RepoService
	publisher  JobPublisher
	maxRetries int
}

func NewBatchService(client *ent.Client, repos *RepoService, publisher JobPublisher, maxRetries int) *BatchService {
	if maxRetries < 1 {
		maxRetries = 3
	}
	return &BatchService{
		client:     client,
		repos:      repos,
		publisher:  publisher,
		maxRetries: maxRetries,
	}
}

// RefreshAllAccepted is returned for POST /api/repos/refresh-all.
type RefreshAllAccepted struct {
	BatchID uuid.UUID `json:"batch_id" swaggertype:"string" format:"uuid"`
}

// StartRefreshAll creates a batch, job rows, and publishes one batch-kick message.
func (s *BatchService) StartRefreshAll(ctx context.Context) (RefreshAllAccepted, error) {
	ids, err := s.repos.ListAllIDs(ctx)
	if err != nil {
		return RefreshAllAccepted{}, err
	}
	batch, err := s.client.RefreshBatch.Create().Save(ctx)
	if err != nil {
		return RefreshAllAccepted{}, err
	}
	if len(ids) == 0 {
		return RefreshAllAccepted{BatchID: batch.ID}, nil
	}

	builders := make([]*ent.RefreshBatchJobCreate, 0, len(ids))
	for _, repoID := range ids {
		builders = append(builders, s.client.RefreshBatchJob.Create().
			SetID(uuid.New()).
			SetBatchID(batch.ID).
			SetRepoID(repoID).
			SetStatus(schema.RefreshJobStatusPending))
	}
	if err := s.client.RefreshBatchJob.CreateBulk(builders...).Exec(ctx); err != nil {
		return RefreshAllAccepted{}, err
	}
	if err := s.publisher.PublishBatchKick(ctx, queue.BatchKick{BatchID: batch.ID}); err != nil {
		// Jobs are already committed; surface batch_id so clients can call the repair endpoint.
		return RefreshAllAccepted{}, apierror.Unavailable(
			fmt.Sprintf("failed to enqueue batch kick; retry via POST /api/batches/%s/enqueue", batch.ID),
		)
	}
	return RefreshAllAccepted{BatchID: batch.ID}, nil
}

// EnqueueBatch republishes a batch kick so the worker can fan out still-pending jobs.
func (s *BatchService) EnqueueBatch(ctx context.Context, batchID uuid.UUID) error {
	exists, err := s.client.RefreshBatch.Query().Where(refreshbatch.IDEQ(batchID)).Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return apierror.NotFound("batch not found")
	}
	if err := s.publisher.PublishBatchKick(ctx, queue.BatchKick{BatchID: batchID}); err != nil {
		return apierror.Unavailable("failed to enqueue batch kick")
	}
	return nil
}

// FanOutBatch publishes a refresh message for every still-pending job in the batch.
// Idempotent under redelivery: already-terminal jobs are skipped; duplicate refresh
// messages are handled by conditional status updates.
func (s *BatchService) FanOutBatch(ctx context.Context, batchID uuid.UUID) error {
	jobs, err := s.client.RefreshBatchJob.Query().
		Where(
			refreshbatchjob.BatchIDEQ(batchID),
			refreshbatchjob.StatusEQ(schema.RefreshJobStatusPending),
		).
		All(ctx)
	if err != nil {
		return queue.NewTransient(err, time.Second)
	}
	for _, j := range jobs {
		msg := queue.RefreshJob{JobID: j.ID, BatchID: j.BatchID, RepoID: j.RepoID}
		if err := s.publisher.PublishRefresh(ctx, msg); err != nil {
			return queue.NewTransient(fmt.Errorf("fan-out job %s: %w", j.ID, err), time.Second)
		}
	}
	return nil
}

// BatchStatus is the pollable batch progress payload.
type BatchStatus struct {
	Total     int               `json:"total"`
	Pending   int               `json:"pending"`
	Succeeded int               `json:"succeeded"`
	Failed    []BatchFailedItem `json:"failed"`
}

// BatchFailedItem records one permanently failed repo refresh.
type BatchFailedItem struct {
	RepoID uuid.UUID `json:"repo_id" swaggertype:"string" format:"uuid"`
	Reason string    `json:"reason"`
}

// GetBatchStatus aggregates job rows for a batch.
func (s *BatchService) GetBatchStatus(ctx context.Context, batchID uuid.UUID) (BatchStatus, error) {
	exists, err := s.client.RefreshBatch.Query().Where(refreshbatch.IDEQ(batchID)).Exist(ctx)
	if err != nil {
		return BatchStatus{}, err
	}
	if !exists {
		return BatchStatus{}, apierror.NotFound("batch not found")
	}

	jobs, err := s.client.RefreshBatchJob.Query().
		Where(refreshbatchjob.BatchIDEQ(batchID)).
		All(ctx)
	if err != nil {
		return BatchStatus{}, err
	}

	out := BatchStatus{
		Total:  len(jobs),
		Failed: make([]BatchFailedItem, 0),
	}
	for _, j := range jobs {
		switch j.Status {
		case schema.RefreshJobStatusPending:
			out.Pending++
		case schema.RefreshJobStatusSucceeded:
			out.Succeeded++
		case schema.RefreshJobStatusFailed:
			reason := ""
			if j.ErrorReason != nil {
				reason = *j.ErrorReason
			}
			out.Failed = append(out.Failed, BatchFailedItem{RepoID: j.RepoID, Reason: reason})
		}
	}
	return out, nil
}

// ProcessRefreshJob refreshes one repo idempotently for a batch job.
// Returns nil on success or already-terminal; queue.TransientError to retry;
// other errors after marking the job failed (permanent).
func (s *BatchService) ProcessRefreshJob(ctx context.Context, job queue.RefreshJob, attempt int) error {
	row, err := s.client.RefreshBatchJob.Get(ctx, job.JobID)
	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("unknown job %s", job.JobID)
		}
		return err
	}
	switch row.Status {
	case schema.RefreshJobStatusSucceeded, schema.RefreshJobStatusFailed:
		return nil
	case schema.RefreshJobStatusPending:
		// continue
	default:
		return fmt.Errorf("unhandled job status %v", row.Status)
	}

	_, err = s.client.RefreshBatchJob.UpdateOneID(job.JobID).
		SetAttempt(attempt).
		Save(ctx)
	if err != nil {
		return err
	}

	_, err = s.repos.Refresh(ctx, job.RepoID)
	if err == nil {
		return s.markSucceeded(ctx, job.JobID)
	}

	if ge, ok := githubclient.As(err); ok {
		metrics.IncGitHubError(githubErrorKindLabel(ge.Kind))
		switch ge.Kind {
		case githubclient.KindRateLimited:
			// Rate limits never burn the retry budget; fleet gate/observer owns cool-down.
			return queue.NewRateLimited(err, ge.RetryAfter)
		case githubclient.KindServer, githubclient.KindNetwork:
			if attempt >= s.maxRetries {
				reason := ge.Error()
				if markErr := s.markFailed(ctx, job.JobID, reason); markErr != nil {
					return markErr
				}
				return fmt.Errorf("%s", reason)
			}
			return queue.NewTransient(err, ge.RetryAfter)
		case githubclient.KindNotFound, githubclient.KindUnauthorized:
			reason := ge.Error()
			if markErr := s.markFailed(ctx, job.JobID, reason); markErr != nil {
				return markErr
			}
			return fmt.Errorf("%s", reason)
		default:
			reason := ge.Error()
			if markErr := s.markFailed(ctx, job.JobID, reason); markErr != nil {
				return markErr
			}
			return fmt.Errorf("%s", reason)
		}
	}

	// Non-github errors from Refresh (e.g. not found repo id) — permanent.
	reason := err.Error()
	if ae, ok := apierror.As(err); ok {
		reason = ae.Message
	}
	if markErr := s.markFailed(ctx, job.JobID, reason); markErr != nil {
		return markErr
	}
	return fmt.Errorf("%s", reason)
}

func (s *BatchService) markSucceeded(ctx context.Context, jobID uuid.UUID) error {
	n, err := s.client.RefreshBatchJob.Update().
		Where(
			refreshbatchjob.IDEQ(jobID),
			refreshbatchjob.StatusEQ(schema.RefreshJobStatusPending),
		).
		SetStatus(schema.RefreshJobStatusSucceeded).
		ClearErrorReason().
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		// Another delivery already finished — idempotent success.
		return nil
	}
	return nil
}

func (s *BatchService) markFailed(ctx context.Context, jobID uuid.UUID, reason string) error {
	n, err := s.client.RefreshBatchJob.Update().
		Where(
			refreshbatchjob.IDEQ(jobID),
			refreshbatchjob.StatusEQ(schema.RefreshJobStatusPending),
		).
		SetStatus(schema.RefreshJobStatusFailed).
		SetErrorReason(reason).
		Save(ctx)
	if err != nil {
		return err
	}
	_ = n
	return nil
}
