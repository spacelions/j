Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set plan.tool=cursor`.
  - Run `./bin/j settings set plan.model=sonnet-4`.
  - Run `./bin/j settings reset plan.tool`.
  - Run `./bin/j settings`.

Expected:
  - The `reset plan.tool` invocation exits with code 0.
  - The final `j settings` listing renders the unknown `plan` bucket
    as a single section after the four known sections, containing
    only `  model = sonnet-4` (single-key reset must leave the rest
    of the bucket intact):

        [plan]
          model = sonnet-4

  - No `tool` row appears under `[plan]`.
