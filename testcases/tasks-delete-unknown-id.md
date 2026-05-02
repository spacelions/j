Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j tasks delete --id ghost-id --yes`.

Expected:
  - Exit code 0 (per `j tasks delete --help`: "Unknown ids print
    `J: no task` and exit 0").
  - Stdout contains the line `J: no task`.
