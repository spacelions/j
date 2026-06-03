package github

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
)

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
	testutil.Init(t)
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
	testutil.Init(t)
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
