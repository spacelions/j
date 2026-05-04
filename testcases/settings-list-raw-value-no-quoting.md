Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.

Steps:
  - Run `./bin/j settings set 'project.must_read=AGENTS.md;CLAUDE.md'`
    (single-quote so the shell preserves the `;`).
  - Run `./bin/j settings`.

Expected:
  - Exit code 0.
  - The listing emits the value as-is, with no TOML-style quoting or
    escaping:

    [project]
      must_read = AGENTS.md;CLAUDE.md
      plan_requires_approval = true

  - No surrounding double quotes, no backslash escapes, no whitespace
    trimming. The two-space indent and the `key = value` separator are
    the only formatting; the value bytes are the bytes stored.
