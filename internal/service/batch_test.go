package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/PavaoZornija1/github-tracker/ent/schema"
	"github.com/PavaoZornija1/github-tracker/internal/ent/enttest"
	"github.com/PavaoZornija1/github-tracker/internal/ent/refreshbatchjob"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/queue"
	"github.com/PavaoZornija1/github-tracker/internal/service"
	"github.com/google/uuid"

	_ "github.com/mattn/go-sqlite3"
)

type memPublisher struct {
	mu   sync.Mutex
	jobs []queue.RefreshJob
}

func (m *memPublisher) PublishRefresh(ctx context.Context, job queue.RefreshJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs = append(m.jobs, job)
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
	n := len(pub.jobs)
	pub.mu.Unlock()
	if n != 1 {
		t.Fatalf("published = %d, want 1", n)
	}
	status, err := batches.GetBatchStatus(context.Background(), res.BatchID)
	if err != nil {
		t.Fatalf("GetBatchStatus: %v", err)
	}
	if status.Total != 1 || status.Pending != 1 {
		t.Fatalf("status = %+v", status)
	}
}
