package store

// KeyPromptPath is the storage key (under BucketPlanner / BucketWorker
// / BucketVerifier) holding a path to a markdown file whose contents
// replace the embedded role prompt at runtime. An unset value keeps
// the bundled `internal/agents/instructions/<role>.md` body in
// effect; `j settings set <role>.prompt=<path>` writes the value and
// (if the file does not yet exist) seeds it with the embedded default.
const KeyPromptPath = "prompt"

// IsRoleBucket reports whether bucket names one of the three role
// buckets (planner, worker, verifier) that participate in the prompt
// override. Centralised here so callers (e.g. the settings/set
// copy-on-set helper) share a single membership rule with the
// prompts package without re-listing the literal triple.
func IsRoleBucket(bucket string) bool {
	switch bucket {
	case BucketPlanner, BucketWorker, BucketVerifier:
		return true
	}
	return false
}
