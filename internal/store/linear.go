package store

// BucketLinear holds Linear-specific settings (the personal API key
// and the default Linear project id). It is created on first write
// from `j settings set linear.…` or from the source picker's link
// flow; absent until the user authenticates once.
const BucketLinear = "linear"

// KeyLinearAPIKey is the storage key (under BucketLinear) for the
// personal Linear API token (`lin_api_…`). User-typed forms
// `linear.api_key` and `linear.api-key` both round-trip to this key
// via the settings storageKey helper. The on-disk form matches
// project.api_key so a single grep finds every secret in the store.
const KeyLinearAPIKey = "api_key"

// KeyLinearProject is the storage key (under BucketLinear) for the
// default Linear project id captured during the link flow. Optional;
// surfaces in `j settings` once set.
const KeyLinearProject = "project"
