package github

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// publicHost is the only PR host accepted in v1. GitHub Enterprise
// support was intentionally dropped: forwarding a personal access
// token to an arbitrary `github.<anything>` hostname would let a
// malicious PR URL steal credentials. Re-add enterprise support
// behind an explicit user-configured allowlist when the schema is
// settled.
const publicHost = "github.com"

// publicEndpoint is the GraphQL endpoint paired with publicHost.
const publicEndpoint = "https://api.github.com/graphql"

// PRRef identifies a single pull request on github.com. It is the
// parsed form of a stored PR URL and the input every other client
// method takes. Number is the integer PR number (1+).
type PRRef struct {
	Host   string
	Owner  string
	Repo   string
	Number int
}

// ParseURL turns a stored PR URL into a PRRef. The PR URL shape is
// the canonical GitHub form:
//
//	https://github.com/<owner>/<repo>/pull/<number>
//	https://github.com/<owner>/<repo>/pulls/<number>
//
// Both `pull` and `pulls` are accepted so a URL captured from the
// web UI (`/pull/`) or from a CLI (`/pulls/`) round-trips. Any host
// other than github.com is rejected as ErrUnsupportedHost so a
// stored URL cannot redirect the user's token to an attacker-
// controlled hostname.
func ParseURL(raw string) (PRRef, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return PRRef{}, fmt.Errorf("%w: %q", ErrInvalidURL, raw)
	}
	if u.Host != publicHost {
		return PRRef{}, fmt.Errorf("%w: %q", ErrUnsupportedHost, raw)
	}
	owner, repo, num, err := splitPRPath(u.Path)
	if err != nil {
		return PRRef{}, fmt.Errorf("%w: %q", ErrInvalidURL, raw)
	}
	return PRRef{
		Host:   u.Host,
		Owner:  owner,
		Repo:   repo,
		Number: num,
	}, nil
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

// Endpoint returns the GraphQL endpoint for github.com. The receiver
// is preserved so tests and future enterprise support can swap to a
// per-ref endpoint without churning every caller.
func (r PRRef) Endpoint() string {
	return publicEndpoint
}
