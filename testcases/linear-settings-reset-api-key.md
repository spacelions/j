Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set linear.api_key=lin_api_TESTTOKEN linear.project=p-1`.
  - Run `./bin/j settings reset linear.api_key`.
  - Run `./bin/j settings`.

Expected:
  - All three commands exit with code 0.
  - The reset command prints `J: unset linear.api_key` (or equivalent).
  - The final listing's `[linear]` section contains only
    `  project = p-1` — the `api_key` row is gone but the `project`
    row is preserved (single-key reset, not bucket-wide).
