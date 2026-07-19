package service

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/PavaoZornija1/github-tracker/internal/apierror"
)

// ListSort is the supported list ordering.
type ListSort string

const (
	SortStarsDesc   ListSort = "stars_desc"
	SortUpdatedDesc ListSort = "updated_desc"
)

func ParseListSort(raw string) (ListSort, error) {
	if raw == "" {
		return SortUpdatedDesc, nil
	}
	switch ListSort(raw) {
	case SortStarsDesc, SortUpdatedDesc:
		return ListSort(raw), nil
	default:
		return "", apierror.Validation("sort must be stars_desc or updated_desc")
	}
}

type listCursor struct {
	Sort      ListSort  `json:"sort"`
	Stars     int       `json:"stars,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	ID        uuid.UUID `json:"id"`
}

type changesCursor struct {
	UpdatedAt time.Time `json:"updated_at"`
	ID        uuid.UUID `json:"id"`
}

func encodeCursor(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func decodeListCursor(raw string, expected ListSort) (*listCursor, error) {
	if raw == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, apierror.Validation("invalid cursor")
	}
	var c listCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, apierror.Validation("invalid cursor")
	}
	if c.Sort != expected {
		return nil, apierror.Validation("cursor does not match sort")
	}
	if c.ID == uuid.Nil {
		return nil, apierror.Validation("invalid cursor")
	}
	return &c, nil
}

func decodeChangesCursor(raw string) (*changesCursor, error) {
	if raw == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, apierror.Validation("invalid since cursor")
	}
	var c changesCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, apierror.Validation("invalid since cursor")
	}
	if c.ID == uuid.Nil || c.UpdatedAt.IsZero() {
		return nil, apierror.Validation("invalid since cursor")
	}
	return &c, nil
}

func clampLimit(limit, def, max int) int {
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}
