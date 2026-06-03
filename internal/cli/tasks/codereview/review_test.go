package codereview

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleFile() ReviewFile {
	return ReviewFile{
		SchemaVersion: SchemaVersion,
		Provider:      "github",
		FetchedAt:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		PR: PR{
			URL: "https://github.com/x/y/pull/1", Owner: "x",
			Repo: "y", Number: 1, State: "open",
		},
		Items: []Item{
			{
				SourceID: "review-comment:1", Kind: "review_comment",
				Author: "alice", Body: "fix nil",
			},
			{
				SourceID: "issue-comment:2", Kind: "conversation_comment",
				Author: "bob", Body: "simplify",
			},
		},
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.toml")
	want := sampleFile()
	require.NoError(t, Save(path, want))
	got, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, want.PR.URL, got.PR.URL)
	require.Len(t, got.Items, 2)
	assert.Equal(t, "review-comment:1", got.Items[0].SourceID)
}

func TestLoad_Missing(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read")
}

func TestLoad_BadTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.toml")
	require.NoError(t, writeRaw(path, "this is = not = valid"))
	_, err := Load(path)
	require.Error(t, err)
}

func TestSnapshotSourceIDs(t *testing.T) {
	got := SnapshotSourceIDs(sampleFile())
	assert.True(t, got["review-comment:1"])
	assert.True(t, got["issue-comment:2"])
	assert.False(t, got["review-comment:99"])
}

func TestValidatePost_Happy(t *testing.T) {
	f := sampleFile()
	f.Decision = "changes_needed"
	f.Items[0].Decision = "accepted"
	f.Items[0].PlanRef = "P1"
	f.Items[0].Reply = "ack"
	f.Items[1].Decision = "rejected"
	require.NoError(t, ValidatePost(f, SnapshotSourceIDs(sampleFile())))
}

func TestValidatePost_InvalidTopDecision(t *testing.T) {
	f := sampleFile()
	f.Decision = "weird"
	err := ValidatePost(f, SnapshotSourceIDs(f))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "top-level decision")
}

func TestValidatePost_MissingTopDecision(t *testing.T) {
	f := sampleFile()
	// decision left empty
	err := ValidatePost(f, SnapshotSourceIDs(f))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestValidatePost_MissingSourceID(t *testing.T) {
	original := SnapshotSourceIDs(sampleFile())
	f := sampleFile()
	f.Decision = "changes_needed"
	f.Items = f.Items[:1] // drop second item
	f.Items[0].Decision = "accepted"
	f.Items[0].PlanRef = "P1"
	err := ValidatePost(f, original)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing source_id")
}

func TestValidatePost_InventedSourceID(t *testing.T) {
	original := SnapshotSourceIDs(sampleFile())
	f := sampleFile()
	f.Decision = "changes_needed"
	f.Items[0].Decision = "accepted"
	f.Items[0].PlanRef = "P1"
	f.Items[1].Decision = "rejected"
	f.Items = append(f.Items, Item{
		SourceID: "review-comment:999",
		Kind:     "review_comment",
		Decision: "non_actionable",
	})
	err := ValidatePost(f, original)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invented source_id")
}

func TestValidatePost_InvalidItemDecision(t *testing.T) {
	f := sampleFile()
	f.Decision = "changes_needed"
	f.Items[0].Decision = "maybe"
	f.Items[1].Decision = "rejected"
	err := ValidatePost(f, SnapshotSourceIDs(f))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid decision "maybe"`)
}

func TestValidatePost_AcceptedNeedsPlanRef(t *testing.T) {
	f := sampleFile()
	f.Decision = "changes_needed"
	f.Items[0].Decision = "accepted"
	// missing plan_ref
	f.Items[1].Decision = "rejected"
	err := ValidatePost(f, SnapshotSourceIDs(f))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing plan_ref")
}

func TestValidatePost_ReplyTooLong(t *testing.T) {
	f := sampleFile()
	f.Decision = "changes_needed"
	f.Items[0].Decision = "accepted"
	f.Items[0].PlanRef = "P1"
	f.Items[0].Reply = strings.Repeat("x", ReplyMaxRunes+1)
	f.Items[1].Decision = "rejected"
	err := ValidatePost(f, SnapshotSourceIDs(f))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestSave_AtomicRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.toml")
	require.NoError(t, Save(path, sampleFile()))
	// no leftover .tmp file
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	assert.Empty(t, matches)
}

func TestSave_WriteError(t *testing.T) {
	// A read-only directory makes os.WriteFile fail at the temp file.
	dir := t.TempDir()
	require.NoError(t, chmod(dir, 0o500))
	defer func() { _ = chmod(dir, 0o700) }()
	path := filepath.Join(dir, "review.toml")
	err := Save(path, sampleFile())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write")
}

func writeRaw(path, body string) error {
	return writeFile(path, []byte(body))
}
