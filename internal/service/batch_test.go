package service_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PavaoZornija1/github-tracker/ent/schema"
	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/ent/enttest"
	"github.com/PavaoZornija1/github-tracker/internal/ent/refreshbatchjob"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/queue"
	"github.com/PavaoZornija1/github-tracker/internal/service"
	"github.com/google/uuid"

	_ "github.com/mattn/go-sqlite3"
)

type memPublisher struct {
	mu    sync.Mutex
	jobs  []queue.RefreshJob
	kicks []queue.BatchKick
	// failRefreshAfter fails PublishRefresh once this many have succeeded (0 = never).
	failRefreshAfter int
	refreshCalls     int
	failKick         bool
}

func (m *memPublisher) PublishRefresh(ctx context.Context, job queue.RefreshJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshCalls++
	if m.failRefreshAfter > 0 && m.refreshCalls > m.failRefreshAfter {
		return fmt.Errorf("publish failed")
	}
	m.jobs = append(m.jobs, job)
	return nil
}

func (m *memPublisher) PublishBatchKick(ctx context.Context, kick queue.BatchKick) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failKick {
		return fmt.Errorf("kick publish failed")
	}
	m.kicks = append(m.kicks, kick)
	return nil
}

type refreshGitHub struct {
	repo  *githubclient.Repo
	err   error
	calls int
}

func (g *refreshGitHub) Get(ctx context.Context, owner, name string) (*githubclient.Repo, error) {
	g.calls++
	if g.err != nil {
		return nil, g.err
	}
	return g.repo, nil
}

func (g *refreshGitHub) Invalidate(ctx context.Context, owner, name string) error {
	return nil
}

func TestProcessRefreshJobIdempotent(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:batchidem?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	repo, err := client.Repository.Create().
		SetOwner("golang").
		SetName("go").
		SetFullName("golang/go").
		SetHTMLURL("https://github.com/golang/go").
		SetStars(1).
		SetFetchedAt(time.Now().UTC()).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	batch, err := client.RefreshBatch.Create().Save(context.Background())
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	jobID := uuid.New()
	_, err = client.RefreshBatchJob.Create().
		SetID(jobID).
		SetBatchID(batch.ID).
		SetRepoID(repo.ID).
		SetStatus(schema.RefreshJobStatusPending).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	gh := &refreshGitHub{repo: &githubclient.Repo{
		Owner:     "golang",
		Name:      "go",
		FullName:  "golang/go",
		Stars:     99,
		HTMLURL:   "https://github.com/golang/go",
		FetchedAt: time.Now().UTC(),
	}}
	repos := service.NewRepoService(client, gh)
	batches := service.NewBatchService(client, repos, &memPublisher{}, nil, 3)

	job := queue.RefreshJob{JobID: jobID, BatchID: batch.ID, RepoID: repo.ID}
	if err := batches.ProcessRefreshJob(context.Background(), job, 1); err != nil {
		t.Fatalf("first process: %v", err)
	}
	if err := batches.ProcessRefreshJob(context.Background(), job, 1); err != nil {
		t.Fatalf("second process: %v", err)
	}

	row, err := client.RefreshBatchJob.Get(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if row.Status != schema.RefreshJobStatusSucceeded {
		t.Fatalf("status = %v", row.Status)
	}
	succeeded, err := client.RefreshBatchJob.Query().
		Where(
			refreshbatchjob.BatchIDEQ(batch.ID),
			refreshbatchjob.StatusEQ(schema.RefreshJobStatusSucceeded),
		).
		Count(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if succeeded != 1 {
		t.Fatalf("succeeded count = %d, want 1", succeeded)
	}
	// GitHub called once: second process short-circuits on terminal status.
	if gh.calls != 1 {
		t.Fatalf("github calls = %d, want 1", gh.calls)
	}
}

func TestStartRefreshAllEnqueuesJobs(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:batchstart?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	_, err := client.Repository.Create().
		SetOwner("golang").
		SetName("go").
		SetFullName("golang/go").
		SetHTMLURL("https://github.com/golang/go").
		SetFetchedAt(time.Now().UTC()).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	pub := &memPublisher{}
	repos := service.NewRepoService(client, &refreshGitHub{})
	batches := service.NewBatchService(client, repos, pub, nil, 3)

	res, err := batches.StartRefreshAll(context.Background())
	if err != nil {
		t.Fatalf("StartRefreshAll: %v", err)
	}
	if res.BatchID == uuid.Nil {
		t.Fatal("expected batch id")
	}
	pub.mu.Lock()
	nJobs := len(pub.jobs)
	nKicks := len(pub.kicks)
	pub.mu.Unlock()
	if nJobs != 0 {
		t.Fatalf("published refresh jobs = %d, want 0 (kick only)", nJobs)
	}
	if nKicks != 1 {
		t.Fatalf("published kicks = %d, want 1", nKicks)
	}
	if err := batches.FanOutBatch(context.Background(), res.BatchID); err != nil {
		t.Fatalf("FanOutBatch: %v", err)
	}
	pub.mu.Lock()
	nJobs = len(pub.jobs)
	pub.mu.Unlock()
	if nJobs != 1 {
		t.Fatalf("after fan-out published = %d, want 1", nJobs)
	}
	status, err := batches.GetBatchStatus(context.Background(), res.BatchID)
	if err != nil {
		t.Fatalf("GetBatchStatus: %v", err)
	}
	if status.Total != 1 || status.Pending != 1 {
		t.Fatalf("status = %+v", status)
	}
}

func TestStartRefreshAllKickFailureExposesBatchID(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:batchkickfail?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	_, err := client.Repository.Create().
		SetOwner("golang").
		SetName("go").
		SetFullName("golang/go").
		SetHTMLURL("https://github.com/golang/go").
		SetFetchedAt(time.Now().UTC()).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	pub := &memPublisher{failKick: true}
	batches := service.NewBatchService(client, service.NewRepoService(client, &refreshGitHub{}), pub, nil, 3)

	_, err = batches.StartRefreshAll(context.Background())
	if err == nil {
		t.Fatal("expected kick publish error")
	}
	ae, ok := apierror.As(err)
	if !ok || ae.Code != apierror.CodeUnavailable {
		t.Fatalf("want unavailable apierror, got %v", err)
	}
	pending, err := client.RefreshBatchJob.Query().
		Where(refreshbatchjob.StatusEQ(schema.RefreshJobStatusPending)).
		All(context.Background())
	if err != nil {
		t.Fatalf("query jobs: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending jobs = %d, want 1", len(pending))
	}
	batchID := pending[0].BatchID.String()
	if !strings.Contains(ae.Message, batchID) {
		t.Fatalf("error message %q does not include batch_id %s for repair", ae.Message, batchID)
	}
	if !strings.Contains(ae.Message, "/api/batches/"+batchID+"/enqueue") {
		t.Fatalf("error message %q missing repair path", ae.Message)
	}
}

func TestGetBatchStatusMixedAggregation(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:batchstatus?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	ctx := context.Background()
	mkRepo := func(full string) uuid.UUID {
		t.Helper()
		owner, name := "o", full
		r, err := client.Repository.Create().
			SetOwner(owner).
			SetName(name).
			SetFullName(full).
			SetHTMLURL("https://github.com/" + full).
			SetFetchedAt(time.Now().UTC()).
			Save(ctx)
		if err != nil {
			t.Fatalf("create repo %s: %v", full, err)
		}
		return r.ID
	}
	pendingID := mkRepo("org/pending")
	okID := mkRepo("org/ok")
	failID := mkRepo("org/fail")

	batch, err := client.RefreshBatch.Create().Save(ctx)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	_, err = client.RefreshBatchJob.Create().
		SetID(uuid.New()).
		SetBatchID(batch.ID).
		SetRepoID(pendingID).
		SetStatus(schema.RefreshJobStatusPending).
		Save(ctx)
	if err != nil {
		t.Fatalf("pending job: %v", err)
	}
	_, err = client.RefreshBatchJob.Create().
		SetID(uuid.New()).
		SetBatchID(batch.ID).
		SetRepoID(okID).
		SetStatus(schema.RefreshJobStatusSucceeded).
		Save(ctx)
	if err != nil {
		t.Fatalf("succeeded job: %v", err)
	}
	failReason := "github: not found"
	_, err = client.RefreshBatchJob.Create().
		SetID(uuid.New()).
		SetBatchID(batch.ID).
		SetRepoID(failID).
		SetStatus(schema.RefreshJobStatusFailed).
		SetErrorReason(failReason).
		Save(ctx)
	if err != nil {
		t.Fatalf("failed job: %v", err)
	}

	batches := service.NewBatchService(client, service.NewRepoService(client, &refreshGitHub{}), &memPublisher{}, nil, 3)
	status, err := batches.GetBatchStatus(ctx, batch.ID)
	if err != nil {
		t.Fatalf("GetBatchStatus: %v", err)
	}
	if status.Total != 3 || status.Pending != 1 || status.Succeeded != 1 {
		t.Fatalf("counts = %+v, want total=3 pending=1 succeeded=1", status)
	}
	if len(status.Failed) != 1 {
		t.Fatalf("failed len = %d, want 1", len(status.Failed))
	}
	if status.Failed[0].RepoID != failID || status.Failed[0].Reason != failReason {
		t.Fatalf("failed item = %+v, want repo_id=%s reason=%q", status.Failed[0], failID, failReason)
	}
}

func TestGetBatchStatusUnknownBatch(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:batchmissing?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	batches := service.NewBatchService(client, service.NewRepoService(client, &refreshGitHub{}), &memPublisher{}, nil, 3)
	_, err := batches.GetBatchStatus(context.Background(), uuid.New())
	ae, ok := apierror.As(err)
	if !ok || ae.Code != apierror.CodeNotFound {
		t.Fatalf("err = %v, want not_found", err)
	}
}

func TestProcessRefreshJob_RateLimitDoesNotExhaustBudget(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:batchratelimit?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	repo, err := client.Repository.Create().
		SetOwner("golang").
		SetName("go").
		SetFullName("golang/go").
		SetHTMLURL("https://github.com/golang/go").
		SetFetchedAt(time.Now().UTC()).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	batch, err := client.RefreshBatch.Create().Save(context.Background())
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	jobID := uuid.New()
	_, err = client.RefreshBatchJob.Create().
		SetID(jobID).
		SetBatchID(batch.ID).
		SetRepoID(repo.ID).
		SetStatus(schema.RefreshJobStatusPending).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	gh := &refreshGitHub{err: &githubclient.Error{
		Kind:       githubclient.KindRateLimited,
		StatusCode: 429,
		Message:    "rate limited",
		RetryAfter: time.Second,
	}}
	batches := service.NewBatchService(client, service.NewRepoService(client, gh), &memPublisher{}, nil, 3)
	job := queue.RefreshJob{JobID: jobID, BatchID: batch.ID, RepoID: repo.ID}

	err = batches.ProcessRefreshJob(context.Background(), job, 3)
	var te *queue.TransientError
	if !queue.AsTransient(err, &te) {
		t.Fatalf("err = %v, want TransientError", err)
	}
	if te.CountAsAttempt {
		t.Fatal("rate-limit transient must not count as attempt")
	}

	row, err := client.RefreshBatchJob.Get(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if row.Status != schema.RefreshJobStatusPending {
		t.Fatalf("status = %v, want pending (not failed at maxRetries)", row.Status)
	}
}

func TestProcessRefreshJob_ServerExhaustsBudget(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:batchserver?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	repo, err := client.Repository.Create().
		SetOwner("golang").
		SetName("go").
		SetFullName("golang/go").
		SetHTMLURL("https://github.com/golang/go").
		SetFetchedAt(time.Now().UTC()).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	batch, err := client.RefreshBatch.Create().Save(context.Background())
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	jobID := uuid.New()
	_, err = client.RefreshBatchJob.Create().
		SetID(jobID).
		SetBatchID(batch.ID).
		SetRepoID(repo.ID).
		SetStatus(schema.RefreshJobStatusPending).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	gh := &refreshGitHub{err: &githubclient.Error{
		Kind:       githubclient.KindServer,
		StatusCode: 500,
		Message:    "boom",
	}}
	batches := service.NewBatchService(client, service.NewRepoService(client, gh), &memPublisher{}, nil, 3)
	job := queue.RefreshJob{JobID: jobID, BatchID: batch.ID, RepoID: repo.ID}

	err = batches.ProcessRefreshJob(context.Background(), job, 3)
	if err == nil {
		t.Fatal("expected permanent error after maxRetries")
	}
	var te *queue.TransientError
	if queue.AsTransient(err, &te) {
		t.Fatal("expected non-transient permanent failure")
	}

	row, err := client.RefreshBatchJob.Get(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if row.Status != schema.RefreshJobStatusFailed {
		t.Fatalf("status = %v, want failed", row.Status)
	}
}

func TestFanOutBatch_RecoversPendingOnly(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:batchfanout?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()

	mkRepo := func(name string) uuid.UUID {
		t.Helper()
		r, err := client.Repository.Create().
			SetOwner("org").
			SetName(name).
			SetFullName("org/"+name).
			SetHTMLURL("https://github.com/org/"+name).
			SetFetchedAt(time.Now().UTC()).
			Save(ctx)
		if err != nil {
			t.Fatalf("create repo: %v", err)
		}
		return r.ID
	}
	pendingA := mkRepo("a")
	pendingB := mkRepo("b")
	done := mkRepo("done")

	batch, err := client.RefreshBatch.Create().Save(ctx)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	for _, repoID := range []uuid.UUID{pendingA, pendingB} {
		_, err = client.RefreshBatchJob.Create().
			SetID(uuid.New()).
			SetBatchID(batch.ID).
			SetRepoID(repoID).
			SetStatus(schema.RefreshJobStatusPending).
			Save(ctx)
		if err != nil {
			t.Fatalf("pending job: %v", err)
		}
	}
	_, err = client.RefreshBatchJob.Create().
		SetID(uuid.New()).
		SetBatchID(batch.ID).
		SetRepoID(done).
		SetStatus(schema.RefreshJobStatusSucceeded).
		Save(ctx)
	if err != nil {
		t.Fatalf("done job: %v", err)
	}

	pub := &memPublisher{failRefreshAfter: 1}
	batches := service.NewBatchService(client, service.NewRepoService(client, &refreshGitHub{}), pub, nil, 3)

	err = batches.FanOutBatch(ctx, batch.ID)
	var te *queue.TransientError
	if !queue.AsTransient(err, &te) {
		t.Fatalf("err = %v, want transient after mid-loop failure", err)
	}
	pub.mu.Lock()
	first := len(pub.jobs)
	pub.mu.Unlock()
	if first != 1 {
		t.Fatalf("published before failure = %d, want 1", first)
	}

	// Simulate kick redelivery: reset fail gate and fan out again — only pending remain.
	pub.mu.Lock()
	pub.failRefreshAfter = 0
	pub.refreshCalls = 0
	pub.jobs = nil
	pub.mu.Unlock()

	if err := batches.FanOutBatch(ctx, batch.ID); err != nil {
		t.Fatalf("retry FanOutBatch: %v", err)
	}
	pub.mu.Lock()
	n := len(pub.jobs)
	pub.mu.Unlock()
	if n != 2 {
		t.Fatalf("republished = %d, want 2 pending only", n)
	}
}
