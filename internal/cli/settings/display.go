package settings

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

// keyTable lists every bucket whose display form differs from its
// storage form. Current keys are identity-mapped; future divergent
// keys can register here without changing list/set/reset callers.
var keyTable = map[string]*keyMap{}
