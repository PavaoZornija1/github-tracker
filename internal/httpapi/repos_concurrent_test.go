package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/ent/enttest"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/httpapi"
	"github.com/PavaoZornija1/github-tracker/internal/service"

	_ "github.com/mattn/go-sqlite3"
)

type stubGitHub struct {
	repo *githubclient.Repo
}

func (s *stubGitHub) Get(ctx context.Context, owner, name string) (*githubclient.Repo, error) {
	return s.repo, nil
}

func (s *stubGitHub) Invalidate(ctx context.Context, owner, name string) error {
	return nil
}

func TestConcurrentPOSTCreateConflict(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:httpconcurrent?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	svc := service.NewRepoService(client, &stubGitHub{
		repo: &githubclient.Repo{
			Owner:     "golang",
			Name:      "go",
			FullName:  "golang/go",
			Stars:     1,
			HTMLURL:   "https://github.com/golang/go",
			FetchedAt: time.Now().UTC(),
		},
	})
	engine := httpapi.NewRouter(httpapi.RouterDeps{
		Repos:    svc,
		Logger: slog.Default(),
	})

	const n = 12
	var wg sync.WaitGroup
	wg.Add(n)
	codes := make(chan int, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			body := bytes.NewBufferString(`{"owner":"golang","name":"go"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/repos", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			codes <- w.Code
			if w.Code != http.StatusCreated && w.Code != http.StatusConflict {
				var env apierror.Envelope
				_ = json.Unmarshal(w.Body.Bytes(), &env)
				t.Errorf("status=%d body=%s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	close(codes)

	var created, conflict int
	for code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
		}
	}
	if created != 1 || conflict != n-1 {
		t.Fatalf("created=%d conflict=%d want 1 and %d", created, conflict, n-1)
	}

	count, err := client.Repository.Query().Count(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows=%d want 1", count)
	}
}
