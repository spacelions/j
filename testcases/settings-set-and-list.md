Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`. Confirm
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
  - The first `j settings` listing contains `plan.tool` and the value
    `cursor`.
  - The second `j settings` listing contains BOTH `plan.tool=cursor`
    AND `plan.model=sonnet-4` (one row per key).
