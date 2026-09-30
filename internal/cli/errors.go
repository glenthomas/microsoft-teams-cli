package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/glenthomas/microsoft-teams-cli/internal/auth"
	"github.com/glenthomas/microsoft-teams-cli/internal/graph"
	"github.com/glenthomas/microsoft-teams-cli/internal/teams"
)

// Exit codes are part of the CLI contract so agents can branch on them.
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitNotFound    = 4
	ExitGraphAPI    = 5
	ExitInterrupted = 130
)

// ErrorInfo is the JSON payload written to stderr on failure.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type usageError struct{ error }

func newUsageError(err error) error { return usageError{err} }

// classify maps an error to a stable error code and process exit code.
func classify(err error) (ErrorInfo, int) {
	info := ErrorInfo{Code: "error", Message: err.Error()}
	var (
		ue       usageError
		apiErr   *graph.APIError
		notFound *teams.NotFoundError
		amb      *teams.AmbiguousError
	)
	switch {
	case errors.As(err, &ue):
		info.Code = "usage"
		return info, ExitUsage
	case errors.Is(err, context.Canceled):
		info.Code = "interrupted"
		return info, ExitInterrupted
	case errors.Is(err, context.DeadlineExceeded):
		info.Code = "timeout"
		return info, ExitError
	case errors.Is(err, auth.ErrNotLoggedIn):
		info.Code = "not_logged_in"
		return info, ExitAuth
	case errors.As(err, &notFound):
		info.Code = "not_found"
		info.Details = notFound
		return info, ExitNotFound
	case errors.As(err, &amb):
		info.Code = "ambiguous"
		info.Details = amb
		return info, ExitNotFound
	case errors.As(err, &apiErr):
		info.Details = apiErr
		switch apiErr.StatusCode {
		case 401:
			info.Code = "not_logged_in"
			info.Message += " (run `teams login`)"
			return info, ExitAuth
		case 403:
			info.Code = "forbidden"
			info.Message += " (the signed-in user or app may lack the required Graph permission/admin consent)"
		case 404:
			info.Code = "not_found"
			return info, ExitNotFound
		case 429:
			info.Code = "throttled"
		default:
			info.Code = "graph_error"
		}
		return info, ExitGraphAPI
	}
	if isCobraUsageError(err) {
		info.Code = "usage"
		return info, ExitUsage
	}
	return info, ExitError
}

func isCobraUsageError(err error) bool {
	msg := err.Error()
	for _, p := range []string{"unknown command", "unknown flag", "unknown shorthand flag", "required flag", "invalid argument", "accepts ", "flag needs an argument"} {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	return false
}
