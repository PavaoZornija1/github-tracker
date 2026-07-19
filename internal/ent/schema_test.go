package ent_test

import (
	"context"
	"testing"
	"time"

	"github.com/PavaoZornija1/github-tracker/ent/schema"
	"github.com/PavaoZornija1/github-tracker/internal/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
)

func TestRepositoryAndBatchSchema(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	ctx := context.Background()
	repo, err := client.Repository.Create().
		SetOwner("golang").
		SetName("go").
		SetFullName("golang/go").
		SetHTMLURL("https://github.com/golang/go").
		SetStars(100).
		SetFetchedAt(time.Now()).
		Save(ctx)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}

	batch, err := client.RefreshBatch.Create().Save(ctx)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}

	job, err := client.RefreshBatchJob.Create().
		SetBatchID(batch.ID).
		SetRepoID(repo.ID).
		SetStatus(schema.RefreshJobStatusPending).
		Save(ctx)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	_, err = client.RefreshBatchJob.Create().
		SetBatchID(batch.ID).
		SetRepoID(repo.ID).
		SetStatus(schema.RefreshJobStatusPending).
		Save(ctx)
	if err == nil {
		t.Fatal("expected unique constraint on (batch_id, repo_id)")
	}

	if job.Status != schema.RefreshJobStatusPending {
		t.Fatalf("status = %v, want pending", job.Status)
	}
}
