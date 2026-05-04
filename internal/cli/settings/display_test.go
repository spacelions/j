package settings

import (
	"testing"

	"github.com/spacelions/j/internal/store"
)

// TestDisplayKey covers the passthrough behaviour for buckets/keys
// without an explicit table entry. Pinning this keeps settings output
// stable while the keyTable is empty.
func TestDisplayKey(t *testing.T) {
	tests := []struct {
		name      string
		bucket    string
		storedKey string
		want      string
	}{
		{
			name:      "project_bucket_passthrough",
			bucket:    store.BucketProject,
			storedKey: "must_read",
			want:      "must_read",
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

// TestStorageKey is the inverse of TestDisplayKey: it covers
// passthrough behaviour for keys without explicit storage/display
// divergence.
func TestStorageKey(t *testing.T) {
	tests := []struct {
		name       string
		bucket     string
		displayKey string
		want       string
	}{
		{
			name:       "project_bucket_passthrough",
			bucket:     store.BucketProject,
			displayKey: "must_read",
			want:       "must_read",
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
