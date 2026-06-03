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
	assert.False(t, ref.IsEnterprise)
	assert.Equal(t, "https://api.github.com/graphql", ref.Endpoint())
}

func TestParseURL_PullsLiteral(t *testing.T) {
	ref, err := ParseURL("https://github.com/acme/app/pulls/7")
	require.NoError(t, err)
	assert.Equal(t, 7, ref.Number)
}

func TestParseURL_Enterprise(t *testing.T) {
	ref, err := ParseURL("https://github.acme.com/team/repo/pull/9")
	require.NoError(t, err)
	assert.True(t, ref.IsEnterprise)
	assert.Equal(t, "https://github.acme.com/api/graphql", ref.Endpoint())
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
