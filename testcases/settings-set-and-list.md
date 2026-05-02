Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set plan.tool=cursor`.
  - Run `./bin/j settings`.
  - Run `./bin/j settings set plan.model=sonnet-4`.
  - Run `./bin/j settings`.

Expected:
  - Each `set` invocation exits with code 0 and writes a confirmation
    line such as `set plan.tool=cursor` (or similar; the exact wording
    is whatever `j settings set` prints — exit code 0 is the
    load-bearing assertion).
  - Listings render in TOML form: a `[plan]` header followed by
    indented `key = value` rows for the unknown `plan` bucket, appended
    after the four known sections (`[project]`, `[planner]`, `[coder]`,
    `[verifier]`).
  - The first `j settings` listing contains the `[plan]` section with
    a row `  tool = cursor`.
  - The second `j settings` listing contains the `[plan]` section with
    BOTH rows in alphabetical key order:

        [plan]
          model = sonnet-4
          tool = cursor
