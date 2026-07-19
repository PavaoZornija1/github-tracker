package githubclient

import "time"

// Repo is the subset of GitHub repository fields we persist and cache.
type Repo struct {
	Owner       string    `json:"owner"`
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description *string   `json:"description"`
	Stars       int       `json:"stargazers_count"`
	Language    *string   `json:"language"`
	HTMLURL     string    `json:"html_url"`
	FetchedAt   time.Time `json:"fetched_at"`
}

type githubAPIRepo struct {
	Name        string  `json:"name"`
	FullName    string  `json:"full_name"`
	Description *string `json:"description"`
	Stars       int     `json:"stargazers_count"`
	Language    *string `json:"language"`
	HTMLURL     string  `json:"html_url"`
	Owner       struct {
		Login string `json:"login"`
	} `json:"owner"`
}
