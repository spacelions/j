Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j work --from-task ghost-id`.

Expected:
  - Non-zero exit code.
  - Output contains `ghost-id` and signals "task not found" (the exact
    wording is the run error from cobra). The store contains no rows,
    so the resolver cannot find the requested id.
