package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

// emptyInitListing is the bare `j settings` output after
// `j init --yes --must-read=`. The four known sections render in
// fixed order; [project] carries the seeded must_read (empty),
// plan_requires_approval, and max_iterations rows.
//
// Pinned by settings-list-empty.md, settings-list-fresh-init-renders-
// four-sections.md, settings-list-blank-separator-between-sections.md,
// settings-list-no-trailing-blank-line.md, and
// linear-settings-list-no-section-on-fresh-init.md.
const emptyInitListing = "[project]\n" +
	"  max_iterations = 3\n" +
	"  must_read = \n" +
	"  plan_requires_approval = true\n" +
	"\n" +
	"[planner]\n" +
	"\n" +
	"[worker]\n" +
	"\n" +
	"[verifier]\n"

// TestSettingsList_FreshInit pins the byte-exact output of the bare
// `j settings` command on a freshly-initialised project: four known
// sections in fixed order, single blank line between them, no trailing
// blank line, no `[linear]` section (the bucket is created on first
// write only).
//
// Replaces testcases/settings-list-empty.md,
// testcases/settings-list-fresh-init-renders-four-sections.md,
// testcases/settings-list-blank-separator-between-sections.md,
// testcases/settings-list-no-trailing-blank-line.md, and
// testcases/linear-settings-list-no-section-on-fresh-init.md.
func TestSettingsList_FreshInit(t *testing.T) {
	freshInit(t)

	stdout, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stdout != emptyInitListing {
		t.Fatalf("stdout = %q, want %q", stdout, emptyInitListing)
	}
	if strings.Contains(stdout, "[linear]") {
		t.Fatalf("fresh init must not surface the [linear] section: %q", stdout)
	}
	if strings.HasSuffix(stdout, "\n\n") {
		t.Fatalf("trailing blank line: %q", stdout)
	}
	if strings.Contains(stdout, "\n\n\n") {
		t.Fatalf("two consecutive blank lines: %q", stdout)
	}
}

// TestSettingsList_KeysSortedWithinSection pins that keys inside a
// section render in alphabetical order regardless of the order they
// were set on the command line.
//
// Replaces testcases/settings-list-keys-sorted-within-section.md.
func TestSettingsList_KeysSortedWithinSection(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "worker.tool=cursor", "worker.model=gpt-5", "worker.interactive=false",
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	stdout, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := "[worker]\n" +
		"  interactive = false\n" +
		"  model = gpt-5\n" +
		"  tool = cursor\n"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout = %q, want substring %q", stdout, want)
	}
}

// TestSettingsList_MatchesTOMLExample pins the listing's user-facing
// TOML layout: two-space indent, `key = value` (raw value, no
// quoting), alphabetised keys, single blank-line separator.
//
// Replaces testcases/settings-list-matches-toml-example.md and
// testcases/settings-list-raw-value-no-quoting.md.
func TestSettingsList_MatchesTOMLExample(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "project.must_read=path/one;path/two",
	); err != nil {
		t.Fatalf("set must_read: %v", err)
	}
	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "planner.tool=cursor", "planner.model=opus", "planner.interactive=false",
	); err != nil {
		t.Fatalf("set planner: %v", err)
	}
	stdout, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := "[project]\n" +
		"  max_iterations = 3\n" +
		"  must_read = path/one;path/two\n" +
		"  plan_requires_approval = true\n" +
		"\n" +
		"[planner]\n" +
		"  interactive = false\n" +
		"  model = opus\n" +
		"  tool = cursor\n" +
		"\n" +
		"[worker]\n" +
		"\n" +
		"[verifier]\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

// TestSettingsList_ResetKeyThenList pins the set/reset/list round-trip
// where a single-key reset leaves the rest of the bucket intact.
//
// Replaces testcases/settings-list-reset-key-then-list.md and the
// equivalent `settings-reset-rep-pick-path.md` re-pick path doc
// (the .Pick branch is exercised by internal/cli/picker tests; here
// we only assert the persisted state the rest of `j` reads from).
func TestSettingsList_ResetKeyThenList(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "planner.tool=cursor",
	); err != nil {
		t.Fatalf("set tool: %v", err)
	}
	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "planner.model=sonnet-4",
	); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if _, _, err := testutil.RunCobra(t, settings.New(),
		"reset", "planner.tool",
	); err != nil {
		t.Fatalf("reset tool: %v", err)
	}
	stdout, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := "[planner]\n" +
		"  model = sonnet-4\n"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout = %q, want substring %q", stdout, want)
	}
	if strings.Contains(stdout, "tool = cursor") {
		t.Fatalf("planner.tool was not unset: %q", stdout)
	}
}

// TestSettingsList_UnknownBucketAfterKnown pins the ordering: known
// sections first (project / planner / worker / verifier), then
// unknown buckets sorted alphabetically.
//
// Replaces testcases/settings-list-unknown-bucket-after-known.md.
func TestSettingsList_UnknownBucketAfterKnown(t *testing.T) {
	freshInit(t)

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "zeta.k=v",
	); err != nil {
		t.Fatalf("set zeta: %v", err)
	}
	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "alpha.x=y",
	); err != nil {
		t.Fatalf("set alpha: %v", err)
	}
	stdout, _, err := testutil.RunCobra(t, settings.New())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := "[verifier]\n" +
		"\n" +
		"[alpha]\n" +
		"  x = y\n" +
		"\n" +
		"[zeta]\n" +
		"  k = v\n"
	if !strings.HasSuffix(stdout, want) {
		t.Fatalf("stdout = %q, want suffix %q", stdout, want)
	}
}
