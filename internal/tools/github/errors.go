// Package github is the GitHub GraphQL client used by
// `j tasks code-review`. It accepts a stored PR URL, derives the
// owner/repo/number triple plus the per-host GraphQL endpoint, and
// fetches conversation comments, review threads, review comments, and
// already-posted j replies for the planner round. Posting back to
// GitHub is intentionally out of scope in v1.
package github

import (
	"errors"
	"fmt"
)

// ErrUnauthorized is returned when the GraphQL endpoint answers 401.
// Code-review callers surface it as a remediation line that names
// every token source j consults (github.token, GITHUB_TOKEN,
// GH_TOKEN). The wording is centralised here so the cli text never
// drifts from the resolver.
var ErrUnauthorized = errors.New(
	"github: unauthorized (configure `github.token`, GITHUB_TOKEN, " +
		"or GH_TOKEN)")

// ErrNotFound is returned when the GraphQL response reports a null
// pullRequest node. The cli wraps this with the original PR URL so
// the user sees "PR not accessible" rather than a vague GraphQL
// message.
var ErrNotFound = errors.New("github: pull request not found or inaccessible")

// ErrClosed is returned when the PR is closed (not merged). Closed
// PRs do not get review rounds in v1 because the planner has no way
// to act on feedback for work that was abandoned.
var ErrClosed = errors.New("github: pull request is closed")

// ErrMerged is returned when the PR is already merged. Merged PRs do
// not get review rounds in v1 because there is nothing left to
// change.
var ErrMerged = errors.New("github: pull request is merged")

// ErrDraft is returned when the PR is still a draft. Draft PRs do
// not get review rounds in v1 because reviewers have not been
// invited to leave feedback yet.
var ErrDraft = errors.New("github: pull request is a draft")

// ErrUnsupportedHost is returned by ParseURL when the URL does not
// belong to a recognised GitHub host (github.com or a GitHub
// Enterprise host whose hostname does not begin with `github.`). The
// error wraps the offending URL so the cli can echo it back.
var ErrUnsupportedHost = errors.New("github: unsupported PR host")

// ErrInvalidURL is returned by ParseURL when the URL is missing the
// owner/repo/pulls/number shape every GitHub PR URL carries.
var ErrInvalidURL = errors.New("github: invalid PR URL")

// HTTPError wraps a non-2xx HTTP response. 401s are mapped to
// ErrUnauthorized before this type is constructed so callers comparing
// via errors.Is keep working; every other status code surfaces as
// *HTTPError so the cli can print the status + body verbatim.
type HTTPError struct {
	Status int
	Body   string
}

// Error renders an *HTTPError as `github: http <status>: <body>` so
// the cli can print it as a single line.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("github: http %d: %s", e.Status, e.Body)
}
