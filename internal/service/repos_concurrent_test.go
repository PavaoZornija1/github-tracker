package service_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/ent/enttest"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/service"

	_ "github.com/mattn/go-sqlite3"
)

type mockGitHub struct {
	repo  *githubclient.Repo
	delay time.Duration
	calls atomic.Int64
}

func (m *mockGitHub) Get(ctx context.Context, owner, name string) (*githubclient.Repo, error) {
	m.calls.Add(1)
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}
	return m.repo, nil
}

func (m *mockGitHub) Invalidate(ctx context.Context, owner, name string) error {
	return nil
}

func TestCreateConcurrentDuplicateReturnsConflict(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:concurrent?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	gh := &mockGitHub{
		delay: 30 * time.Millisecond,
		repo: &githubclient.Repo{
			Owner:     "golang",
			Name:      "go",
			FullName:  "golang/go",
			Stars:     100,
			HTMLURL:   "https://github.com/golang/go",
			FetchedAt: time.Now().UTC(),
		},
	}
	svc := service.NewRepoService(client, gh)

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.Create(context.Background(), "golang", "go")
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var created, conflicts, other int
	for err := range results {
		switch {
		case err == nil:
			created++
		default:
			ae, ok := apierror.As(err)
			if ok && ae.Code == apierror.CodeConflict {
				conflicts++
				continue
			}
			other++
			t.Errorf("unexpected error: %v", err)
		}
	}

	if created != 1 {
		t.Fatalf("created = %d, want 1", created)
	}
	if conflicts != n-1 {
		t.Fatalf("conflicts = %d, want %d", conflicts, n-1)
	}
	if other != 0 {
		t.Fatalf("other errors = %d", other)
	}

	count, err := client.Repository.Query().Count(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows = %d, want 1", count)
	}
}

func TestCreatePersistsFetchedFields(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:createok?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	desc := "The Go programming language"
	lang := "Go"
	svc := service.NewRepoService(client, &mockGitHub{
		repo: &githubclient.Repo{
			Owner:       "golang",
			Name:        "go",
			FullName:    "golang/go",
			Description: &desc,
			Stars:       42,
			Language:    &lang,
			HTMLURL:     "https://github.com/golang/go",
			FetchedAt:   time.Now().UTC(),
		},
	})

	dto, err := svc.Create(context.Background(), "golang", "go")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if dto.FullName != "golang/go" || dto.Stars != 42 {
		t.Fatalf("dto = %+v", dto)
	}
	if _, err := client.Repository.Get(context.Background(), dto.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
}
