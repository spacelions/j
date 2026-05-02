Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j tasks enter` (no `--id`, no `--print`) on the
    freshly-initialised, empty task store.

Expected:
  - Exit code 0 (per `j tasks enter --help`: "an empty store prints
    `J: no tasks`").
  - Stdout contains the line `J: no tasks`.
  - No huh picker is rendered (the empty-store branch short-circuits
    before the picker).
