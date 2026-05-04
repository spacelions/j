Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.
  - Run `./bin/j settings set linear.api_key=lin_api_TESTTOKEN` so
    the missing-key branch does not fire first.

Steps:
  - Run `./bin/j plan --from-linear foo`.
  - Run `./bin/j plan --from-linear eng-123` (lowercase team prefix).
  - Run `./bin/j plan --from-linear ENG` (missing dash + counter).
  - Run `./bin/j plan --from-linear 1NG-123` (numeric leading character).
  - Run `./bin/j plan --from-linear A-1` (single-letter team prefix
    rejected because the validator requires at least two prefix chars).

Expected:
  - Every invocation exits with a non-zero code.
  - Each prints a single user-facing error line containing
    `linear: invalid identifier (expected pattern like ENG-123)` and
    quotes the offending identifier verbatim.
  - No task is created: `./bin/j tasks` reports `J: no tasks` and
    `.j/tasks/` contains no per-id subdirectory.
