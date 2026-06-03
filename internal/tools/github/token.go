package github

import (
	"os"
	"strings"

	"github.com/spacelions/j/internal/store"
)

// envTokenVars lists the env-var names consulted before the stored
// `github.token` value. GITHUB_TOKEN is the canonical Actions env
// var; GH_TOKEN is what `gh` uses. Order is the precedence: the
// first non-empty wins so users can shadow a stored token from a
// shell session without resetting the store.
var envTokenVars = []string{"GITHUB_TOKEN", "GH_TOKEN"}

// ResolveToken returns the access token for code-review GraphQL
// calls. Lookup order is GITHUB_TOKEN, GH_TOKEN, then the stored
// `github.token` value under store.BucketGithub. An empty result
// means no token was configured; callers surface that as an
// ErrUnauthorized-style hint pointing the user at the same trio of
// sources.
//
// The store handle is opened and closed inside the call so the
// caller never has to plumb one through; the bbolt file lock is
// not held across the GraphQL round-trip.
func ResolveToken() string {
	for _, name := range envTokenVars {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return readStoredToken()
}

func readStoredToken() string {
	path := store.DefaultPath()
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	s, err := store.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = s.Close() }()
	v, _, err := s.Get(store.BucketGithub, store.KeyGithubToken)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}
