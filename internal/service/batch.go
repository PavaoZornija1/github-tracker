package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/PavaoZornija1/github-tracker/ent/schema"
	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/ent"
	"github.com/PavaoZornija1/github-tracker/internal/ent/refreshbatch"
	"github.com/PavaoZornija1/github-tracker/internal/ent/refreshbatchjob"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/queue"
)

const githubRateLimitKey = "github:rate_limit_until"

// JobPublisher enqueues refresh jobs.
type JobPublisher interface {
	PublishRefresh(ctx context.Context, job queue.RefreshJob) error
}

// BatchService manages refresh batches and idempotent job processing.
type BatchService struct {
	client     *ent.Client
	repos      *RepoService
	publisher  JobPublisher
	rdb        *redis.Client
	maxRetries int
}

func NewBatchService(client *ent.Client, repos *RepoService, publisher JobPublisher, rdb *redis.Client, maxRetries int) *BatchService {
	if maxRetries < 1 {
		maxRetries = 3
	}
	return &BatchService{
		client:     client,
		repos:      repos,
		publisher:  publisher,
		rdb:        rdb,
		maxRetries: maxRetries,
	}
}

// RefreshAllAccepted is returned for POST /api/repos/refresh-all.
type RefreshAllAccepted struct {
	BatchID uuid.UUID `json:"batch_id" swaggertype:"string" format:"uuid"`
}

// StartRefreshAll creates a batch, job rows, and enqueues one message per repo.
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
	jobs := make([]queue.RefreshJob, 0, len(ids))
	for _, repoID := range ids {
		jobID := uuid.New()
		builders = append(builders, s.client.RefreshBatchJob.Create().
			SetID(jobID).
			SetBatchID(batch.ID).
			SetRepoID(repoID).
			SetStatus(schema.RefreshJobStatusPending))
		jobs = append(jobs, queue.RefreshJob{
			JobID:   jobID,
			BatchID: batch.ID,
			RepoID:  repoID,
		})
	}
	if err := s.client.RefreshBatchJob.CreateBulk(builders...).Exec(ctx); err != nil {
		return RefreshAllAccepted{}, err
	}
	for _, job := range jobs {
		if err := s.publisher.PublishRefresh(ctx, job); err != nil {
			return RefreshAllAccepted{}, fmt.Errorf("enqueue refresh job %s: %w", job.JobID, err)
		}
	}
	return RefreshAllAccepted{BatchID: batch.ID}, nil
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

	if err := s.waitOutRateLimit(ctx); err != nil {
		return err
	}

	_, err = s.repos.Refresh(ctx, job.RepoID)
	if err == nil {
		return s.markSucceeded(ctx, job.JobID)
	}

	if ge, ok := githubclient.As(err); ok {
		switch ge.Kind {
		case githubclient.KindRateLimited, githubclient.KindServer, githubclient.KindNetwork:
			if ge.Kind == githubclient.KindRateLimited && ge.RetryAfter > 0 && s.rdb != nil {
				_ = s.rdb.Set(ctx, githubRateLimitKey, time.Now().Add(ge.RetryAfter).UTC().Format(time.RFC3339Nano), ge.RetryAfter).Err()
			}
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

func (s *BatchService) waitOutRateLimit(ctx context.Context) error {
	if s.rdb == nil {
		return nil
	}
	val, err := s.rdb.Get(ctx, githubRateLimitKey).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return queue.NewTransient(err, time.Second)
	}
	until, err := time.Parse(time.RFC3339Nano, val)
	if err != nil {
		return nil
	}
	delay := time.Until(until)
	if delay <= 0 {
		return nil
	}
	return queue.NewTransient(fmt.Errorf("backing off github rate limit"), delay)
}
