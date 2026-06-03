package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURL_PublicGitHub(t *testing.T) {
	ref, err := ParseURL("https://github.com/acme/app/pull/42")
	require.NoError(t, err)
	assert.Equal(t, "github.com", ref.Host)
	assert.Equal(t, "acme", ref.Owner)
	assert.Equal(t, "app", ref.Repo)
	assert.Equal(t, 42, ref.Number)
}

func TestParseURL_PullsLiteral(t *testing.T) {
	ref, err := ParseURL("https://github.com/acme/app/pulls/7")
	require.NoError(t, err)
	assert.Equal(t, 7, ref.Number)
}

// TestParseURL_RejectsEnterpriseHost pins the v1 security contract:
// even hostnames that look like GitHub Enterprise (github.acme.com)
// are rejected so a stored PR URL cannot redirect the user's token
// to an attacker-controlled host.
func TestParseURL_RejectsEnterpriseHost(t *testing.T) {
	_, err := ParseURL("https://github.acme.com/team/repo/pull/9")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedHost)
}

func TestParseURL_RejectsAttackerHost(t *testing.T) {
	_, err := ParseURL("https://github.attacker.tld/o/r/pull/1")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedHost)
}

func TestParseURL_UnsupportedHost(t *testing.T) {
	_, err := ParseURL("https://gitlab.com/team/repo/pull/1")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedHost)
}

func TestParseURL_InvalidShape(t *testing.T) {
	cases := []string{
		"https://github.com/acme/app/issues/3",
		"https://github.com/acme/app/pull/zero",
		"https://github.com/acme/app/pull/-1",
		"https://github.com/acme",
		"not a url",
		"",
	}
	for _, raw := range cases {
		_, err := ParseURL(raw)
		assert.Error(t, err, "input %q must fail", raw)
	}
}
