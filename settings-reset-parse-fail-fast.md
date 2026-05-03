Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.

Steps:
  - Run `./bin/j settings set worker.model=sonnet planner.tool=cursor`.
  - Run `./bin/j settings reset worker.model .key`.
  - Run `./bin/j settings`.

Expected:
  - The `reset worker.model .key` invocation exits with a non-zero code
    (parse error on `.key`: bucket portion empty). Stderr/stdout
    contains `settings: bucket name must be non-empty in ".key"`.
  - Because `parseResetTargets` runs BEFORE the store is opened, the
    valid `worker.model` target is NOT applied: the final
    `j settings` listing still shows the `[worker]` section with
    `model = sonnet` and `[planner]` with `tool = cursor`. No partial
    state.

Pins: requirement "Validation errors for malformed `bucket.key`
(`.key`, `bucket.`) still surface as today" + plan step 5 ("call
parseResetTargets first so a parse error fails fast before opening
the DB").
