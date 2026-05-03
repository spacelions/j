Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.

Steps:
  - Without setting any planner.* keys first, run
    `./bin/j settings reset planner`.
  - Run `./bin/j settings reset bucket.ghost` (key under a never-
    created bucket).

Expected:
  - The `reset planner` invocation exits with code 0 and stdout is
    exactly:

        unset planner

    (no error: missing-bucket reset is a no-op success that mirrors
    the existing missing-key behaviour of single-key reset.)
  - The `reset bucket.ghost` invocation exits with code 0 and stdout
    is exactly:

        unset bucket.ghost

  - `j settings` afterwards still renders the four known sections
    (`[project]`, `[planner]`, `[worker]`, `[verifier]`) with no rows
    under planner/worker/verifier.

Pins: requirement "Missing bucket / missing key is still a no-op
success (matches current `Delete` semantics)".
