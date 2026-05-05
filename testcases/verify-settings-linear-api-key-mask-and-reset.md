Prerequisites:
  - From the worktree root, run `make build` to compile `./bin/j`.
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.

Steps:
  - Confirm a fresh `./bin/j settings list` does NOT yet render a
    `[linear]` section (the four default sections only).
  - Set the Linear API key in snake_case form:
      ./bin/j settings set linear.api_key=lin_api_test
  - Run `./bin/j settings list` and capture the output.
  - Set the same value via the kebab-case alias:
      ./bin/j settings set linear.api-key=lin_api_kebab
  - Run `./bin/j settings list` and capture the output again — the
    storage key must be the same `api_key`, no duplicate row.
  - Reset:
      ./bin/j settings reset linear.api_key
  - Run `./bin/j settings list` once more.

Expected:
  - Initial `settings list` shows exactly four sections —
    `[project]`, `[planner]`, `[worker]`, `[verifier]` — and no
    `[linear]` block.
  - After either set form, `[linear]` appears with one row:
      api_key = ****
    (the literal four-asterisk mask, never the cleartext token).
  - The kebab and snake forms map to the same storage key — the
    second `settings list` still has a single masked `api_key` row,
    not two.
  - After `reset linear.api_key`, the `[linear]` section is gone
    entirely (no empty header, no stub row).
