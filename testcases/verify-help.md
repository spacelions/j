Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j verify --help`.
  - Run `./bin/j verify resume --help`.

Expected:
  - Both invocations exit with code 0.
  - `verify --help` stdout mentions `verifier`, `--from-task`,
    `--from-settings`, `--interactive`, `--max-iterations`, and lists
    the `resume` subcommand.
  - `verify resume --help` stdout mentions the `--from-task` flag and
    `VERIFY_RESUME_FROM_TASK` env-var binding, plus the permissive
    eligibility filter.
