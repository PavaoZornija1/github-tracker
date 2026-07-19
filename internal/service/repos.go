package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/ent"
	"github.com/PavaoZornija1/github-tracker/internal/ent/predicate"
	"github.com/PavaoZornija1/github-tracker/internal/ent/refreshbatchjob"
	"github.com/PavaoZornija1/github-tracker/internal/ent/repository"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
)

// GitHubGateway fetches and invalidates cached GitHub repo payloads.
type GitHubGateway interface {
	Get(ctx context.Context, owner, name string) (*githubclient.Repo, error)
	Invalidate(ctx context.Context, owner, name string) error
}

// RepoService implements watchlist use cases.
type RepoService struct {
	client *ent.Client
	github GitHubGateway
}

func NewRepoService(client *ent.Client, github GitHubGateway) *RepoService {
	return &RepoService{client: client, github: github}
}

// RepoDTO is the API-facing repository record.
type RepoDTO struct {
	ID          uuid.UUID `json:"id"`
	Owner       string    `json:"owner"`
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description *string   `json:"description"`
	Stars       int       `json:"stars"`
	Language    *string   `json:"language"`
	HTMLURL     string    `json:"html_url"`
	Notes       string    `json:"notes"`
	FetchedAt   time.Time `json:"fetched_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toDTO(r *ent.Repository) RepoDTO {
	return RepoDTO{
		ID:          r.ID,
		Owner:       r.Owner,
		Name:        r.Name,
		FullName:    r.FullName,
		Description: r.Description,
		Stars:       r.Stars,
		Language:    r.Language,
		HTMLURL:     r.HTMLURL,
		Notes:       r.Notes,
		FetchedAt:   r.FetchedAt,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// Create fetches from GitHub and persists. Concurrent duplicates return conflict.
func (s *RepoService) Create(ctx context.Context, owner, name string) (RepoDTO, error) {
	owner = trim(owner)
	name = trim(name)
	if owner == "" || name == "" {
		return RepoDTO{}, apierror.Validation("owner and name are required")
	}

	gh, err := s.github.Get(ctx, owner, name)
	if err != nil {
		return RepoDTO{}, MapGitHubError(err)
	}

	create := s.client.Repository.Create().
		SetOwner(gh.Owner).
		SetName(gh.Name).
		SetFullName(gh.FullName).
		SetStars(gh.Stars).
		SetHTMLURL(gh.HTMLURL).
		SetFetchedAt(gh.FetchedAt)
	if gh.Description != nil {
		create.SetDescription(*gh.Description)
	}
	if gh.Language != nil {
		create.SetLanguage(*gh.Language)
	}

	row, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return RepoDTO{}, apierror.Conflict("repository already tracked")
		}
		return RepoDTO{}, err
	}
	return toDTO(row), nil
}

// Get returns one repository by id.
func (s *RepoService) Get(ctx context.Context, id uuid.UUID) (RepoDTO, error) {
	row, err := s.client.Repository.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return RepoDTO{}, apierror.NotFound("repository not found")
		}
		return RepoDTO{}, err
	}
	return toDTO(row), nil
}

// ListParams controls filtered cursor pagination.
type ListParams struct {
	Language string
	Sort     ListSort
	Cursor   string
	Limit    int
}

// ListResult is a page of repositories plus optional next cursor.
type ListResult struct {
	Items      []RepoDTO `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// List returns a keyset page of repositories.
func (s *RepoService) List(ctx context.Context, p ListParams) (ListResult, error) {
	sort, err := ParseListSort(string(p.Sort))
	if err != nil {
		return ListResult{}, err
	}
	if p.Sort == "" {
		sort = SortUpdatedDesc
	}
	limit := clampLimit(p.Limit, 20, 100)
	cur, err := decodeListCursor(p.Cursor, sort)
	if err != nil {
		return ListResult{}, err
	}

	q := s.client.Repository.Query()
	if p.Language != "" {
		q = q.Where(repository.LanguageEQ(p.Language))
	}

	switch sort {
	case SortStarsDesc:
		if cur != nil {
			q = q.Where(starsCursorPredicate(cur.Stars, cur.ID))
		}
		q = q.Order(ent.Desc(repository.FieldStars), ent.Desc(repository.FieldID))
	case SortUpdatedDesc:
		if cur != nil {
			q = q.Where(updatedCursorPredicate(cur.UpdatedAt, cur.ID))
		}
		q = q.Order(ent.Desc(repository.FieldUpdatedAt), ent.Desc(repository.FieldID))
	default:
		return ListResult{}, apierror.Validation("unsupported sort")
	}

	rows, err := q.Limit(limit + 1).All(ctx)
	if err != nil {
		return ListResult{}, err
	}

	var next string
	if len(rows) > limit {
		last := rows[limit-1]
		rows = rows[:limit]
		switch sort {
		case SortStarsDesc:
			next, err = encodeCursor(listCursor{Sort: sort, Stars: last.Stars, ID: last.ID})
		case SortUpdatedDesc:
			next, err = encodeCursor(listCursor{Sort: sort, UpdatedAt: last.UpdatedAt, ID: last.ID})
		}
		if err != nil {
			return ListResult{}, err
		}
	}

	items := make([]RepoDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, toDTO(r))
	}
	return ListResult{Items: items, NextCursor: next}, nil
}

func starsCursorPredicate(stars int, id uuid.UUID) predicate.Repository {
	return repository.Or(
		repository.StarsLT(stars),
		repository.And(repository.StarsEQ(stars), repository.IDLT(id)),
	)
}

func updatedCursorPredicate(ts time.Time, id uuid.UUID) predicate.Repository {
	return repository.Or(
		repository.UpdatedAtLT(ts),
		repository.And(repository.UpdatedAtEQ(ts), repository.IDLT(id)),
	)
}

// UpdateNotes patches the user-editable notes field.
func (s *RepoService) UpdateNotes(ctx context.Context, id uuid.UUID, notes string) (RepoDTO, error) {
	row, err := s.client.Repository.UpdateOneID(id).
		SetNotes(notes).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return RepoDTO{}, apierror.NotFound("repository not found")
		}
		return RepoDTO{}, err
	}
	return toDTO(row), nil
}

// Delete removes a repository from the watchlist.
func (s *RepoService) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.client.RefreshBatchJob.Delete().Where(refreshbatchjob.RepoIDEQ(id)).Exec(ctx)
	if err != nil {
		return err
	}
	err = s.client.Repository.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return apierror.NotFound("repository not found")
		}
		return err
	}
	return nil
}

// Refresh re-fetches GitHub data synchronously and updates stored fields.
func (s *RepoService) Refresh(ctx context.Context, id uuid.UUID) (RepoDTO, error) {
	existing, err := s.client.Repository.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return RepoDTO{}, apierror.NotFound("repository not found")
		}
		return RepoDTO{}, err
	}

	if err := s.github.Invalidate(ctx, existing.Owner, existing.Name); err != nil {
		return RepoDTO{}, err
	}
	gh, err := s.github.Get(ctx, existing.Owner, existing.Name)
	if err != nil {
		return RepoDTO{}, err
	}

	upd := s.client.Repository.UpdateOneID(id).
		SetOwner(gh.Owner).
		SetName(gh.Name).
		SetFullName(gh.FullName).
		SetStars(gh.Stars).
		SetHTMLURL(gh.HTMLURL).
		SetFetchedAt(gh.FetchedAt)
	if gh.Description != nil {
		upd.SetDescription(*gh.Description)
	} else {
		upd.ClearDescription()
	}
	if gh.Language != nil {
		upd.SetLanguage(*gh.Language)
	} else {
		upd.ClearLanguage()
	}

	row, err := upd.Save(ctx)
	if err != nil {
		return RepoDTO{}, err
	}
	return toDTO(row), nil
}

// Stats is aggregated watchlist metrics computed in SQL.
type Stats struct {
	Total       int     `json:"total"`
	TotalStars  int     `json:"total_stars"`
	TopLanguage *string `json:"top_language"`
}

// Stats returns SQL aggregates without loading all rows into memory.
func (s *RepoService) Stats(ctx context.Context) (Stats, error) {
	var agg []struct {
		Count int `json:"count"`
		Sum   int `json:"sum"`
	}
	err := s.client.Repository.Query().
		Aggregate(
			ent.Count(),
			ent.Sum(repository.FieldStars),
		).
		Scan(ctx, &agg)
	if err != nil {
		return Stats{}, err
	}

	out := Stats{}
	if len(agg) > 0 {
		out.Total = agg[0].Count
		out.TotalStars = agg[0].Sum
	}
	if out.Total == 0 {
		return out, nil
	}

	var langs []struct {
		Language *string `json:"language"`
		Count    int     `json:"count"`
	}
	err = s.client.Repository.Query().
		Where(repository.LanguageNotNil()).
		GroupBy(repository.FieldLanguage).
		Aggregate(ent.Count()).
		Scan(ctx, &langs)
	if err != nil {
		return Stats{}, err
	}
	var bestCount int
	for _, row := range langs {
		if row.Language == nil {
			continue
		}
		if row.Count > bestCount {
			bestCount = row.Count
			lang := *row.Language
			out.TopLanguage = &lang
		}
	}
	return out, nil
}

// ChangesParams controls the change-feed poller.
type ChangesParams struct {
	Since string
	Limit int
}

// ChangesResult is an at-least-once page of changed repos.
type ChangesResult struct {
	Items      []RepoDTO `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// Changes returns repos with (updated_at, id) greater than the since cursor.
// Guarantee: at-least-once for pollers (duplicates OK; gaps are not).
func (s *RepoService) Changes(ctx context.Context, p ChangesParams) (ChangesResult, error) {
	limit := clampLimit(p.Limit, 20, 100)
	cur, err := decodeChangesCursor(p.Since)
	if err != nil {
		return ChangesResult{}, err
	}

	q := s.client.Repository.Query().
		Order(ent.Asc(repository.FieldUpdatedAt), ent.Asc(repository.FieldID))
	if cur != nil {
		q = q.Where(repository.Or(
			repository.UpdatedAtGT(cur.UpdatedAt),
			repository.And(repository.UpdatedAtEQ(cur.UpdatedAt), repository.IDGT(cur.ID)),
		))
	}

	rows, err := q.Limit(limit + 1).All(ctx)
	if err != nil {
		return ChangesResult{}, err
	}

	var next string
	if len(rows) > limit {
		last := rows[limit-1]
		rows = rows[:limit]
		next, err = encodeCursor(changesCursor{UpdatedAt: last.UpdatedAt, ID: last.ID})
		if err != nil {
			return ChangesResult{}, err
		}
	} else if len(rows) > 0 {
		last := rows[len(rows)-1]
		next, err = encodeCursor(changesCursor{UpdatedAt: last.UpdatedAt, ID: last.ID})
		if err != nil {
			return ChangesResult{}, err
		}
	}

	items := make([]RepoDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, toDTO(r))
	}
	return ChangesResult{Items: items, NextCursor: next}, nil
}

// ListAllIDs returns every tracked repo id (used by refresh-all enqueue).
func (s *RepoService) ListAllIDs(ctx context.Context) ([]uuid.UUID, error) {
	return s.client.Repository.Query().IDs(ctx)
}

func trim(s string) string {
	return strings.TrimSpace(s)
}
