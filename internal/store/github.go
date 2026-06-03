package store

// BucketGithub holds GitHub-specific settings (the personal API token
// used by `j tasks code-review`). It is created on first write from
// `j settings set github.token=…`; absent until the user configures
// it. Token configuration is intentionally NOT prompted by `j init`.
const BucketGithub = "github"

// KeyGithubToken is the storage key (under BucketGithub) for the
// personal GitHub access token. Masked in `j settings` output the
// same way KeyProjectAPIKey and KeyLinearAPIKey are. Token discovery
// for code-review falls back to GITHUB_TOKEN and GH_TOKEN env vars
// before reading this stored value.
const KeyGithubToken = "token"
