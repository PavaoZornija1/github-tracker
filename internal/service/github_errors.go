package service

import (
	"github.com/PavaoZornija1/github-tracker/internal/apierror"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
)

// MapGitHubError converts typed GitHub failures into API errors.
func MapGitHubError(err error) error {
	ge, ok := githubclient.As(err)
	if !ok {
		return err
	}
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
