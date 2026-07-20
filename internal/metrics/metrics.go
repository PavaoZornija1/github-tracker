package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests by method, status code, and route path template.",
		},
		[]string{"method", "code", "path"},
	)

	GitHubErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "github_errors_total",
			Help: "Total GitHub client errors by kind.",
		},
		[]string{"kind"},
	)
)

// IncGitHubError increments github_errors_total for a classified kind label.
func IncGitHubError(kind string) {
	if kind == "" {
		kind = "unknown"
	}
	GitHubErrorsTotal.WithLabelValues(kind).Inc()
}
