package githubclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimitInfo is parsed from GitHub rate-limit response headers.
type RateLimitInfo struct {
	Remaining  int // -1 if unknown
	ResetAt    time.Time
	RetryAfter time.Duration
}

// RateLimitObserver records rate-limit headers for fleet-wide gating.
type RateLimitObserver interface {
	OnRateLimit(ctx context.Context, info RateLimitInfo)
}

// Client talks to the GitHub REST API with an explicit timeout and typed errors.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
	userAgent  string
	observer   RateLimitObserver
}

// Options configures a Client.
type Options struct {
	BaseURL    string
	Token      string
	Timeout    time.Duration
	HTTPClient *http.Client
	UserAgent  string
	Observer   RateLimitObserver
}

// New builds a GitHub API client.
func New(opts Options) *Client {
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = "github-tracker/1.0"
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    base,
		token:      opts.Token,
		userAgent:  ua,
		observer:   opts.Observer,
	}
}

// GetRepo fetches https://api.github.com/repos/{owner}/{name}.
func (c *Client) GetRepo(ctx context.Context, owner, name string) (*Repo, error) {
	if owner == "" || name == "" {
		return nil, &Error{Kind: KindUnknown, Message: "owner and name are required", StatusCode: http.StatusBadRequest}
	}

	url := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, owner, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, networkError(err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, networkError(err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, networkError(err)
	}

	info := parseRateLimitInfo(res.Header)
	c.notifyObserver(ctx, info)

	remaining := info.Remaining
	retryAfter := info.RetryAfter

	switch {
	case res.StatusCode == http.StatusOK:
		var raw githubAPIRepo
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, networkError(fmt.Errorf("decode response: %w", err))
		}
		return &Repo{
			Owner:       raw.Owner.Login,
			Name:        raw.Name,
			FullName:    raw.FullName,
			Description: raw.Description,
			Stars:       raw.Stars,
			Language:    raw.Language,
			HTMLURL:     raw.HTMLURL,
			FetchedAt:   time.Now().UTC(),
		}, nil
	case res.StatusCode == http.StatusNotFound:
		return nil, notFound("repository not found")
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		// 403 can also be rate-limit secondary; prefer rate-limit when remaining is 0 or Retry-After set.
		if res.StatusCode == http.StatusForbidden && (remaining == 0 || retryAfter > 0 || res.Header.Get("X-RateLimit-Remaining") == "0") {
			return nil, rateLimited("github rate limit exceeded", retryAfter, remaining)
		}
		if res.StatusCode == http.StatusUnauthorized {
			return nil, unauthorized("github unauthorized")
		}
		return nil, unauthorized("github forbidden")
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, rateLimited("github rate limit exceeded", retryAfter, remaining)
	case res.StatusCode >= 500:
		return nil, serverError(res.StatusCode, fmt.Sprintf("github server error (%d)", res.StatusCode))
	default:
		return nil, &Error{
			Kind:       KindUnknown,
			StatusCode: res.StatusCode,
			Message:    fmt.Sprintf("unexpected github status %d", res.StatusCode),
		}
	}
}

func (c *Client) notifyObserver(ctx context.Context, info RateLimitInfo) {
	if c == nil || c.observer == nil {
		return
	}
	if info.Remaining < 0 && info.ResetAt.IsZero() && info.RetryAfter <= 0 {
		return
	}
	c.observer.OnRateLimit(ctx, info)
}

func parseRateLimitInfo(h http.Header) RateLimitInfo {
	remaining := parseRemaining(h.Get("X-RateLimit-Remaining"))
	retryAfter := parseRetryAfter(h.Get("Retry-After"))
	resetAt := parseReset(h.Get("X-RateLimit-Reset"))
	if retryAfter <= 0 && remaining == 0 && !resetAt.IsZero() {
		if d := time.Until(resetAt); d > 0 {
			retryAfter = d
		}
	}
	return RateLimitInfo{Remaining: remaining, ResetAt: resetAt, RetryAfter: retryAfter}
}

func parseRemaining(v string) int {
	if v == "" {
		return -1
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return n
}

func parseReset(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
