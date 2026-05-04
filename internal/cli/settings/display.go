package settings

import (
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
)

// displayKey maps a bbolt storage key to the kebab-cased form users see
// in `j settings`. Centralising the mapping here means callers never
// need to know about the camelCase storage form: the rest of the
// codebase deals with the storage key (e.g. resolver.KeyMustRead == "mustRead")
// and only this package translates to/from the user-facing display.
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
// kebab key (from `j settings set` / `j settings reset`) to the bbolt
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

// mustReadDisplay is the kebab-cased form of resolver.KeyMustRead shown to
// users in `j settings`. Centralising the literal here keeps the
// display/storage round-trip in lockstep with the storage const.
const mustReadDisplay = "must-read"

// keyTable lists every bucket whose display form differs from its
// storage form. Today only the project bucket has one such key
// (`mustRead` ↔ `must-read`); future entries register here.
var keyTable = map[string]*keyMap{
	store.BucketProject: {
		toDisplay: map[string]string{resolver.KeyMustRead: mustReadDisplay},
		toStorage: map[string]string{mustReadDisplay: resolver.KeyMustRead},
	},
}
