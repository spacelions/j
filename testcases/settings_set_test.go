package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

// TestSettingsSet_SingleArg pins the basic `j settings set planner.tool=cursor`
// path: exit 0, echoed `J: set planner.tool = cursor`, listing renders
// the row.
//
// Replaces testcases/settings-set-single-arg-unchanged.md.
func TestSettingsSet_SingleArg(t *testing.T) {
	freshInit(t)

	stdout, _, err := testutil.RunCobra(settings.New(),
		"set", "planner.tool=cursor",
	)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.Contains(stdout, "set planner.tool = cursor") {
		t.Fatalf("stdout = %q, want echo line", stdout)
	}
	listing, _, err := testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listing, "[planner]\n  tool = cursor\n") {
		t.Fatalf("listing = %q, want planner.tool row", listing)
	}
}

// TestSettingsSet_MultiPair pins `set a.b=1 c.d=2`: both echoes,
// both buckets show up sorted.
//
// Replaces testcases/settings-set-multi-pair.md.
func TestSettingsSet_MultiPair(t *testing.T) {
	freshInit(t)

	stdout, _, err := testutil.RunCobra(settings.New(),
		"set", "a.b=1", "c.d=2",
	)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.Contains(stdout, "set a.b = 1") {
		t.Fatalf("stdout missing first echo: %q", stdout)
	}
	if !strings.Contains(stdout, "set c.d = 2") {
		t.Fatalf("stdout missing second echo: %q", stdout)
	}
	listing, _, err := testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listing, "[a]\n  b = 1\n") {
		t.Fatalf("listing missing [a]: %q", listing)
	}
	if !strings.Contains(listing, "[c]\n  d = 2\n") {
		t.Fatalf("listing missing [c]: %q", listing)
	}
	if strings.Index(listing, "[a]") > strings.Index(listing, "[c]") {
		t.Fatalf("[a] should come before [c]: %q", listing)
	}
}

// TestSettingsSet_DuplicateKeyLastWins pins that two writes to the
// same key in one batch produce two echo lines but a single stored
// value (the second wins).
//
// Replaces testcases/settings-set-duplicate-key-last-wins.md.
func TestSettingsSet_DuplicateKeyLastWins(t *testing.T) {
	freshInit(t)

	stdout, _, err := testutil.RunCobra(settings.New(),
		"set", "dup.k=1", "dup.k=2",
	)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.Contains(stdout, "set dup.k = 1") || !strings.Contains(stdout, "set dup.k = 2") {
		t.Fatalf("stdout missing both echo lines: %q", stdout)
	}
	listing, _, err := testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listing, "[dup]\n  k = 2\n") {
		t.Fatalf("listing = %q, want k=2 (last wins)", listing)
	}
	if strings.Contains(listing, "k = 1") {
		t.Fatalf("first write should have been overwritten: %q", listing)
	}
}

// TestSettingsSet_NoArgs pins cobra's MinimumNArgs(1) error wording.
//
// Replaces testcases/settings-set-no-args.md.
func TestSettingsSet_NoArgs(t *testing.T) {
	freshInit(t)

	_, stderr, err := testutil.RunCobra(settings.New(), "set")
	if err == nil {
		t.Fatal("expected error for missing args")
	}
	if !strings.Contains(err.Error(), "requires at least 1 arg(s), only received 0") &&
		!strings.Contains(stderr, "requires at least 1 arg(s), only received 0") {
		t.Fatalf("err=%v stderr=%q, want cobra arg-count message", err, stderr)
	}
}

// TestSettingsSet_ParseFailsBeforeWrites pins that one malformed pair
// in a batch aborts the batch entirely (no partial state) and reports
// the offending arg.
//
// Replaces testcases/settings-set-parse-fails-before-writes.md.
func TestSettingsSet_ParseFailsBeforeWrites(t *testing.T) {
	freshInit(t)

	_, _, err := testutil.RunCobra(settings.New(),
		"set", "a.b=1", "bad-no-equals", "c.d=2",
	)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), `"bad-no-equals"`) || !strings.Contains(err.Error(), "missing '='") {
		t.Fatalf("err=%v, want mention of bad arg + missing '='", err)
	}
	listing, _, lerr := testutil.RunCobra(settings.New())
	if lerr != nil {
		t.Fatalf("list: %v", lerr)
	}
	if strings.Contains(listing, "[a]") || strings.Contains(listing, "[c]") {
		t.Fatalf("partial state leaked: %q", listing)
	}
}

// TestSettingsSet_MustReadOverwrite pins `project.must_read` writes:
// first store the multi-path value, then overwrite it with a single
// path. The case of the value is preserved verbatim.
//
// Replaces testcases/settings-set-must-read.md and
// testcases/must-read-case-preserved.md.
func TestSettingsSet_MustReadOverwrite(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(settings.New(),
		"set", "project.must_read=AGENTS.md;CLAUDE.md",
	); err != nil {
		t.Fatalf("first set: %v", err)
	}
	listing, _, err := testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("first list: %v", err)
	}
	if !strings.Contains(listing, "  must_read = AGENTS.md;CLAUDE.md\n") {
		t.Fatalf("listing = %q, want multi-path must_read", listing)
	}

	if _, _, err := testutil.RunCobra(settings.New(),
		"set", "project.must_read=AGENTS.md",
	); err != nil {
		t.Fatalf("second set: %v", err)
	}
	listing, _, err = testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("second list: %v", err)
	}
	if !strings.Contains(listing, "  must_read = AGENTS.md\n") {
		t.Fatalf("listing = %q, want overwritten must_read", listing)
	}
	if strings.Contains(listing, "CLAUDE.md") {
		t.Fatalf("previous value not overwritten: %q", listing)
	}

	// Mixed-case value must be preserved verbatim.
	if _, _, err := testutil.RunCobra(settings.New(),
		"set", "project.must_read=AGENTS.md;ClAuDe.MD",
	); err != nil {
		t.Fatalf("mixed-case set: %v", err)
	}
	listing, _, err = testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("mixed-case list: %v", err)
	}
	if !strings.Contains(listing, "  must_read = AGENTS.md;ClAuDe.MD\n") {
		t.Fatalf("listing = %q, want case-preserved must_read", listing)
	}
}

// TestSettingsSet_AndList pins the user-visible flow of
// `set` then `j settings`, including the unknown-bucket ([plan])
// rendering after the four known sections.
//
// Replaces testcases/settings-set-and-list.md.
func TestSettingsSet_AndList(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(settings.New(),
		"set", "plan.tool=cursor",
	); err != nil {
		t.Fatalf("set tool: %v", err)
	}
	listing, _, err := testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("first list: %v", err)
	}
	if !strings.Contains(listing, "[plan]\n  tool = cursor\n") {
		t.Fatalf("listing = %q, want [plan] tool row", listing)
	}

	if _, _, err := testutil.RunCobra(settings.New(),
		"set", "plan.model=sonnet-4",
	); err != nil {
		t.Fatalf("set model: %v", err)
	}
	listing, _, err = testutil.RunCobra(settings.New())
	if err != nil {
		t.Fatalf("second list: %v", err)
	}
	want := "[plan]\n" +
		"  model = sonnet-4\n" +
		"  tool = cursor\n"
	if !strings.Contains(listing, want) {
		t.Fatalf("listing = %q, want sorted [plan] block %q", listing, want)
	}
}
