Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set dup.k=1 dup.k=2`.
  - Run `./bin/j settings`.

Expected:
  - The `set` invocation exits with code 0.
  - Stdout shows two confirmation lines, in order:
      `set dup.k = 1`
      `set dup.k = 2`
  - The `j settings` listing shows `dup.k = 2` (the second write
    overwrote the first; no special error for duplicates).
