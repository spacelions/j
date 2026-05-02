Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j work --help`.
  - Run `./bin/j work resume --help`.

Expected:
  - Both invocations exit with code 0.
  - `work --help` stdout mentions `--from-task`, `--from-file` (alias
    `-f`), `--from-settings`, `--interactive`, and lists the `resume`
    subcommand.
  - `work resume --help` stdout mentions the `--from-task` flag and
    `WORK_RESUME_FROM_TASK` env-var binding, and explains the
    permissive eligibility (any status as long as
    `work_resume_cursor` is non-empty).
