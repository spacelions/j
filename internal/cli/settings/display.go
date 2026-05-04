package settings

import "github.com/spacelions/j/internal/store"

// displayKey maps a bbolt storage key to the form users see in
// `j settings`. Centralising the mapping here means callers can keep
// using storage keys while this package handles any future divergence
// between storage and display names.
//
// Buckets / keys with no entry in the table fall through verbatim so
// adding a new project setting only requires registering the mapping
// when display and storage forms diverge.
func displayKey(bucket, storedKey string) string {
	if m := keyTable[bucket]; m != nil {
		if d, ok := m.toDisplay[storedKey]; ok {
			return d
		}
	}
	return storedKey
}

// storageKey is the inverse of displayKey: it translates a user-typed
// key (from `j settings set` / `j settings reset`) to the bbolt
// storage form. Unknown keys pass through verbatim so unrelated
// bucket.key pairs remain set/reset-able without listing every key
// here.
func storageKey(bucket, displayKey string) string {
	if m := keyTable[bucket]; m != nil {
		if s, ok := m.toStorage[displayKey]; ok {
			return s
		}
	}
	return displayKey
}

// keyMap pairs the per-bucket display↔storage tables. Each bucket
// keeps its own mapping so a bucket with no divergence (e.g. planner)
// has no entry at all.
type keyMap struct {
	toDisplay map[string]string
	toStorage map[string]string
}

// isSecretKey reports whether the (bucket, storedKey) pair carries a
// user-secret that should be masked in `j settings` output. The
// project's Gemini API key and the Linear personal token both qualify.
func isSecretKey(bucket, storedKey string) bool {
	switch bucket {
	case store.BucketProject:
		return storedKey == "api_key"
	case store.BucketLinear:
		return storedKey == store.KeyLinearAPIKey
	}
	return false
}

// keyTable lists every bucket whose display form differs from its
// storage form. The Linear bucket stores the API token under the
// camelCase "apiKey" key but accepts both `linear.api_key` and
// `linear.api-key` from the user; the inverse maps the stored
// "apiKey" back to the kebab-friendly "api_key" line in
// `j settings`.
var keyTable = map[string]*keyMap{
	store.BucketLinear: {
		toDisplay: map[string]string{
			store.KeyLinearAPIKey: "api_key",
		},
		toStorage: map[string]string{
			"api_key": store.KeyLinearAPIKey,
			"api-key": store.KeyLinearAPIKey,
		},
	},
}
