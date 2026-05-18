package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsRoleBucket_Table pins the membership rule shared by the
// settings/set copy-on-set helper and the prompts package.
func TestIsRoleBucket_Table(t *testing.T) {
	cases := []struct {
		name   string
		bucket string
		want   bool
	}{
		{"planner", BucketPlanner, true},
		{"worker", BucketWorker, true},
		{"verifier", BucketVerifier, true},
		{"project", BucketProject, false},
		{"linear", BucketLinear, false},
		{"empty", "", false},
		{"random", "ghost", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsRoleBucket(tc.bucket))
		})
	}
}

// TestKeyPromptPath_Constant pins the storage key value: changing it
// would invalidate every existing user's `<role>.prompt` setting and
// silently break the override chain.
func TestKeyPromptPath_Constant(t *testing.T) {
	assert.Equal(t, "prompt", KeyPromptPath)
}
