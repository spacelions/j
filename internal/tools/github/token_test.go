package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacelions/j/internal/store"
)

// initProject is the local equivalent of testutil.Init.
// internal/testutil imports internal/resolver which now imports
// internal/tools/github (via the FetchAndWriteReview helper), so
// the github tests cannot use testutil without an import cycle.
func initProject(t *testing.T) {
	t.Helper()
	require.NoError(t, store.EnsureProject())
	s, err := store.Open(store.DefaultPath())
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, s.Put(store.BucketProject, "must_read", ""))
}

func TestResolveToken_GitHubTokenWins(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-1")
	t.Setenv("GH_TOKEN", "env-2")
	assert.Equal(t, "env-1", ResolveToken())
}

func TestResolveToken_GHTokenFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "env-2")
	assert.Equal(t, "env-2", ResolveToken())
}

func TestResolveToken_StoredFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Chdir(t.TempDir())
	initProject(t)
	s, err := store.Open(store.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketGithub, store.KeyGithubToken, "stored"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "stored", ResolveToken())
}

func TestResolveToken_NoneConfigured(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Chdir(t.TempDir())
	assert.Empty(t, ResolveToken())
}

func TestResolveToken_EnvPrecedenceOverStore(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-wins")
	t.Chdir(t.TempDir())
	initProject(t)
	s, err := store.Open(store.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketGithub, store.KeyGithubToken, "stored"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "env-wins", ResolveToken())
}
