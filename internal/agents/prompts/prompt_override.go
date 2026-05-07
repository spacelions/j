package prompts

import (
	"fmt"
	"os"

	"github.com/spacelions/j/internal/agents/instructions"
	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store"
)

// roleEntry pairs the bbolt bucket name with the embedded markdown
// returned when no override is configured (or when the override path
// fails to read). The table is the single source of truth used by
// Resolve and by the settings/set copy-on-set helper.
type roleEntry struct {
	bucket string
	embed  string
}

var roleTable = map[string]roleEntry{
	store.BucketPlanner: {
		bucket: store.BucketPlanner, embed: instructions.Planner,
	},
	store.BucketWorker: {
		bucket: store.BucketWorker, embed: instructions.Worker,
	},
	store.BucketVerifier: {
		bucket: store.BucketVerifier, embed: instructions.Verifier,
	},
}

// EmbeddedDefault returns the bundled markdown body for role. An
// unknown role yields the empty string so callers (settings/set) can
// simply skip non-role buckets without a separate membership check.
func EmbeddedDefault(role string) string {
	if entry, ok := roleTable[role]; ok {
		return entry.embed
	}
	return ""
}

// Resolve returns the effective prompt body for role. The role must
// be one of "planner", "worker", "verifier"; any other value yields
// the empty string so misuse is loud at the call site rather than
// silently returning a wrong prompt.
//
// Precedence:
//  1. `<role>.prompt` set in the local settings store AND the file at
//     that path is readable → file contents.
//  2. Anything else → the embedded `instructions.<Role>` body.
//
// On a configured-but-unreadable override Resolve writes a single
// orange warning box to stderr so the user sees that their override
// silently fell back to the bundled default.
func Resolve(role string) string {
	entry, ok := roleTable[role]
	if !ok {
		return ""
	}
	path, ok := lookupPromptPath(entry.bucket)
	if !ok {
		return entry.embed
	}
	body, err := os.ReadFile(path)
	if err != nil {
		warnPromptOverride(
			"read %s.prompt %q: %v", role, path, err,
		)
		return entry.embed
	}
	return string(body)
}

// lookupPromptPath returns the configured prompt path for bucket. Any
// failure to open the store or read the key is treated as "no
// override" — the workflow continues with the embedded default and
// no warning is emitted, since "store not yet initialised" is a
// normal precondition (e.g. unit tests that exercise the prompts
// package without an `.j/settings` file present).
func lookupPromptPath(bucket string) (string, bool) {
	dbPath, err := store.DefaultPath()
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(dbPath); err != nil {
		return "", false
	}
	s, err := store.Open(dbPath)
	if err != nil {
		return "", false
	}
	defer func() { _ = s.Close() }()
	val, ok, err := s.Get(bucket, store.KeyPromptPath)
	if err != nil || !ok || val == "" {
		return "", false
	}
	return val, true
}

// warnPromptOverride surfaces a single orange warning box on stderr.
// The framing matches uitheme.DangerousDialogBox so other warnings
// (failed persistence, missing artifacts) read uniformly. It is
// intentionally best-effort: a write failure on stderr is silent,
// since a workflow that cannot warn cannot do anything useful with
// that fact either.
func warnPromptOverride(format string, a ...any) {
	msg := "J: prompt override: " + fmt.Sprintf(format, a...)
	uitheme.DangerousDialogBox(os.Stderr, "%s", msg)
}
