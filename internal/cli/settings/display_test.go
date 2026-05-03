package settings

import (
	"testing"

	"github.com/spacelions/j/internal/store"
)

// TestDisplayKey covers the three branches of displayKey: a registered
// bucket+storage pair (project.mustRead -> "must-read"), a registered
// bucket with an unregistered storage key (passthrough), and an
// unregistered bucket (passthrough). Pinning these explicitly keeps the
// settings list/reset/set output stable when new project keys are
// added without an explicit table entry.
func TestDisplayKey(t *testing.T) {
	tests := []struct {
		name      string
		bucket    string
		storedKey string
		want      string
	}{
		{
			name:      "registered_bucket_and_key",
			bucket:    store.BucketProject,
			storedKey: "mustRead",
			want:      "must-read",
		},
		{
			name:      "registered_bucket_unmapped_key",
			bucket:    store.BucketProject,
			storedKey: "model",
			want:      "model",
		},
		{
			name:      "unregistered_bucket",
			bucket:    "planner",
			storedKey: "model",
			want:      "model",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := displayKey(tc.bucket, tc.storedKey); got != tc.want {
				t.Fatalf("displayKey(%q, %q) = %q, want %q", tc.bucket, tc.storedKey, got, tc.want)
			}
		})
	}
}

// TestStorageKey is the inverse of TestDisplayKey: it covers the three
// branches of storageKey so a user-typed display key like
// "project.must-read" maps to the bbolt storage form "mustRead",
// while unregistered buckets/keys pass through unchanged.
func TestStorageKey(t *testing.T) {
	tests := []struct {
		name       string
		bucket     string
		displayKey string
		want       string
	}{
		{
			name:       "registered_bucket_and_key",
			bucket:     store.BucketProject,
			displayKey: "must-read",
			want:       "mustRead",
		},
		{
			name:       "registered_bucket_unmapped_key",
			bucket:     store.BucketProject,
			displayKey: "model",
			want:      "model",
		},
		{
			name:       "unregistered_bucket",
			bucket:     "planner",
			displayKey: "model",
			want:       "model",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := storageKey(tc.bucket, tc.displayKey); got != tc.want {
				t.Fatalf("storageKey(%q, %q) = %q, want %q", tc.bucket, tc.displayKey, got, tc.want)
			}
		})
	}
}
