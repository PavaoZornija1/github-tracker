package service

import (
	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/metrics"
)

// MapGitHubError converts typed GitHub failures into API errors.
func MapGitHubError(err error) error {
	ge, ok := githubclient.As(err)
	if !ok {
		return err
	}
	metrics.IncGitHubError(githubErrorKindLabel(ge.Kind))
	switch ge.Kind {
	case githubclient.KindNotFound:
		return apierror.NotFound("github repository not found")
	case githubclient.KindUnauthorized:
		return apierror.Unauthorized("github unauthorized")
	case githubclient.KindRateLimited:
		return apierror.RateLimited("github rate limit exceeded")
	case githubclient.KindServer:
		return apierror.BadGateway("github server error")
	case githubclient.KindNetwork:
		return apierror.Unavailable("github unreachable")
	default:
		return apierror.BadGateway("github request failed")
	}
}

func githubErrorKindLabel(k githubclient.Kind) string {
	switch k {
	case githubclient.KindNotFound:
		return "not_found"
	case githubclient.KindUnauthorized:
		return "unauthorized"
	case githubclient.KindRateLimited:
		return "rate_limited"
	case githubclient.KindServer:
		return "server"
	case githubclient.KindNetwork:
		return "network"
	default:
		return "unknown"
	}
}
