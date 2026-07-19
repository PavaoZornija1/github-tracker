package service_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/ent/enttest"
	"github.com/PavaoZornija1/github-tracker/internal/service"
	"github.com/google/uuid"

	_ "github.com/mattn/go-sqlite3"
)

func TestChangesCursorPaginationNoGaps(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:changespage?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	ctx := context.Background()
	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	// Same updated_at for first two → must order by id ascending (tie-break).
	tieA := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	tieB := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	later := uuid.MustParse("00000000-0000-4000-8000-000000000003")
	latest := uuid.MustParse("00000000-0000-4000-8000-000000000004")

	mk := func(id uuid.UUID, name string, updatedAt time.Time) {
		t.Helper()
		_, err := client.Repository.Create().
			SetID(id).
			SetOwner("org").
			SetName(name).
			SetFullName("org/" + name).
			SetHTMLURL("https://github.com/org/" + name).
			SetFetchedAt(updatedAt).
			SetUpdatedAt(updatedAt).
			Save(ctx)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	mk(tieA, "a", base)
	mk(tieB, "b", base)
	mk(later, "c", base.Add(time.Hour))
	mk(latest, "d", base.Add(2*time.Hour))

	svc := service.NewRepoService(client, &refreshGitHub{})

	wantOrder := []uuid.UUID{tieA, tieB, later, latest}
	seen := make(map[uuid.UUID]int)
	var since string
	for page := 0; page < 10; page++ {
		res, err := svc.Changes(ctx, service.ChangesParams{Since: since, Limit: 2})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(res.Items) == 0 {
			break
		}
		for _, item := range res.Items {
			seen[item.ID]++
		}
		if res.NextCursor == "" {
			break
		}
		since = res.NextCursor
	}

	for _, id := range wantOrder {
		if seen[id] == 0 {
			t.Fatalf("missing repo %s across pages", id)
		}
	}
	if len(seen) != len(wantOrder) {
		t.Fatalf("seen %d distinct repos, want %d", len(seen), len(wantOrder))
	}

	// First page must tie-break by id when updated_at matches.
	first, err := svc.Changes(ctx, service.ChangesParams{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != tieA || first.Items[1].ID != tieB {
		t.Fatalf("tie-break order = %v %v, want %s then %s",
			first.Items[0].ID, first.Items[1].ID, tieA, tieB)
	}

	// Re-polling the same cursor is at-least-once: duplicates are OK.
	again, err := svc.Changes(ctx, service.ChangesParams{Since: first.NextCursor, Limit: 2})
	if err != nil {
		t.Fatalf("re-poll: %v", err)
	}
	dup, err := svc.Changes(ctx, service.ChangesParams{Since: first.NextCursor, Limit: 2})
	if err != nil {
		t.Fatalf("re-poll again: %v", err)
	}
	if len(again.Items) == 0 || len(dup.Items) == 0 {
		t.Fatal("expected items on re-poll")
	}
	if again.Items[0].ID != dup.Items[0].ID {
		t.Fatalf("re-poll mismatch: %s vs %s", again.Items[0].ID, dup.Items[0].ID)
	}
}

func TestChangesInvalidSince(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:changesinvalid?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	svc := service.NewRepoService(client, &refreshGitHub{})
	_, err := svc.Changes(context.Background(), service.ChangesParams{Since: "!!!not-a-cursor"})
	ae, ok := apierror.As(err)
	if !ok || ae.Code != apierror.CodeValidation {
		t.Fatalf("err = %v, want validation", err)
	}

	// Valid base64 JSON but missing required fields.
	raw, _ := json.Marshal(map[string]any{"id": uuid.Nil.String()})
	bad := base64.RawURLEncoding.EncodeToString(raw)
	_, err = svc.Changes(context.Background(), service.ChangesParams{Since: bad})
	ae, ok = apierror.As(err)
	if !ok || ae.Code != apierror.CodeValidation {
		t.Fatalf("err = %v, want validation for empty cursor fields", err)
	}
}
