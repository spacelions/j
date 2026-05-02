Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set a.b=1 c.d=2`.
  - Run `./bin/j settings`.

Expected:
  - The `set` invocation exits with code 0.
  - Stdout of `set` contains, in order, the lines:
      `set a.b = 1`
      `set c.d = 2`
  - The `j settings` listing renders the four known sections first
    (`[project]`, `[planner]`, `[worker]`, `[verifier]`) and then
    appends the two unknown buckets in alphabetical order:

        [a]
          b = 1
        
        [c]
          d = 2
