Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.
  - Confirm `linear.api_key` is NOT set: `./bin/j settings | rg -i linear`
    shows nothing (or no `api_key` row).

Steps:
  - Run `./bin/j plan --from-linear ENG-123` (no token in store).
  - Capture stderr / stdout and exit code.

Expected:
  - Exit code is non-zero.
  - The output is exactly one user-facing error line:
    `J: linear: no API key set; run `j settings set linear.api_key=<lin_api_…>``
    (the `J: ` banner prefix may differ slightly but the
    `linear: no API key set` substring and the suggested `j settings set`
    command must be present).
  - No task is created: `./bin/j tasks` reports `J: no tasks` and the
    `.j/tasks/` directory contains no new `<id>/` subdirectory.
