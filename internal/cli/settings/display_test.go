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
		{
			name:       "linear_api_key_snake_case",
			bucket:     store.BucketLinear,
			displayKey: "api_key",
			want:       store.KeyLinearAPIKey,
		},
		{
			name:       "linear_api_key_kebab_case",
			bucket:     store.BucketLinear,
			displayKey: "api-key",
			want:       store.KeyLinearAPIKey,
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

// TestDisplayKey_Linear pins the inverse direction of the linear
// bucket mapping: the camelCase storage key renders as `api_key` in
// `j settings`.
func TestDisplayKey_Linear(t *testing.T) {
	if got := displayKey(store.BucketLinear, store.KeyLinearAPIKey); got != "api_key" {
		t.Fatalf("displayKey(linear, %s) = %q, want api_key", store.KeyLinearAPIKey, got)
	}
	if got := displayKey(store.BucketLinear, store.KeyLinearProject); got != store.KeyLinearProject {
		t.Fatalf("displayKey(linear, %s) = %q, want passthrough", store.KeyLinearProject, got)
	}
}

// TestIsSecretKey covers both project.api_key (legacy Gemini key)
// and the new linear.apiKey storage key.
func TestIsSecretKey(t *testing.T) {
	if !isSecretKey(store.BucketProject, "api_key") {
		t.Fatal("project.api_key should be masked")
	}
	if !isSecretKey(store.BucketLinear, store.KeyLinearAPIKey) {
		t.Fatal("linear.apiKey should be masked")
	}
	if isSecretKey(store.BucketLinear, store.KeyLinearProject) {
		t.Fatal("linear.project should not be masked")
	}
	if isSecretKey("planner", "tool") {
		t.Fatal("planner.tool should not be masked")
	}
}
