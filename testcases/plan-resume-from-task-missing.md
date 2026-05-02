Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j plan resume --from-task ghost-id`.

Expected:
  - Non-zero exit code (cobra surfaces the run error).
  - Stderr contains the substring `task "ghost-id" not found`.
  - Output mentions `J:` as the user-facing prefix on the not-found
    line.
