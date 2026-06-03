package github

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// PRRef identifies a single pull request on a single GitHub host. It
// is the parsed form of a stored PR URL and the input every other
// client method takes. Host is the bare hostname (no scheme, no
// trailing slash); IsEnterprise marks GitHub Enterprise hosts so the
// endpoint resolver can pick the per-host `/api/graphql` path. Number
// is the integer PR number (1+).
type PRRef struct {
	Host         string
	Owner        string
	Repo         string
	Number       int
	IsEnterprise bool
}

// ParseURL turns a stored PR URL into a PRRef. The PR URL shape is
// the canonical GitHub form:
//
//	https://<host>/<owner>/<repo>/pull/<number>
//	https://<host>/<owner>/<repo>/pulls/<number>
//
// Both `pull` and `pulls` are accepted so a URL captured from the
// web UI (`/pull/`) or from a CLI (`/pulls/`) round-trips. Hosts that
// are not `github.com` are treated as GitHub Enterprise when the
// hostname begins with `github.` (e.g. `github.acme.com`); anything
// else is rejected as ErrUnsupportedHost so unrelated forge URLs do
// not silently get GraphQL traffic.
func ParseURL(raw string) (PRRef, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return PRRef{}, fmt.Errorf("%w: %q", ErrInvalidURL, raw)
	}
	if !isGitHubHost(u.Host) {
		return PRRef{}, fmt.Errorf("%w: %q", ErrUnsupportedHost, raw)
	}
	owner, repo, num, err := splitPRPath(u.Path)
	if err != nil {
		return PRRef{}, fmt.Errorf("%w: %q", ErrInvalidURL, raw)
	}
	return PRRef{
		Host:         u.Host,
		Owner:        owner,
		Repo:         repo,
		Number:       num,
		IsEnterprise: u.Host != "github.com",
	}, nil
}

// isGitHubHost is the host-allowlist used by ParseURL. github.com is
// the public host; any other host whose name begins with `github.` is
// treated as GitHub Enterprise so the GraphQL endpoint resolves to
// `https://<host>/api/graphql` instead of the public api subdomain.
func isGitHubHost(host string) bool {
	if host == "github.com" {
		return true
	}
	return strings.HasPrefix(host, "github.")
}

// splitPRPath validates that path matches the
// `/<owner>/<repo>/{pull|pulls}/<number>` shape and returns its
// pieces. Any deviation surfaces as ErrInvalidURL so the cli can
// echo the original URL back.
func splitPRPath(path string) (owner, repo string, number int, err error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 {
		return "", "", 0, ErrInvalidURL
	}
	if parts[2] != "pull" && parts[2] != "pulls" {
		return "", "", 0, ErrInvalidURL
	}
	n, convErr := strconv.Atoi(parts[3])
	if convErr != nil || n <= 0 {
		return "", "", 0, ErrInvalidURL
	}
	if parts[0] == "" || parts[1] == "" {
		return "", "", 0, ErrInvalidURL
	}
	return parts[0], parts[1], n, nil
}

// Endpoint returns the GraphQL endpoint URL for this PR's host. For
// github.com the public api subdomain is used; for GitHub Enterprise
// hosts the per-host `/api/graphql` path is used so the same
// installation hosts both REST and GraphQL.
func (r PRRef) Endpoint() string {
	if r.Host == "github.com" {
		return "https://api.github.com/graphql"
	}
	return "https://" + r.Host + "/api/graphql"
}
