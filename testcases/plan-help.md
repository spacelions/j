Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j plan --help`.
  - Run `./bin/j plan resume --help`.

Expected:
  - Both invocations exit with code 0.
  - `plan --help` stdout mentions `--from-file` (alias `-f`),
    `--tool`, `--model`, `--interactive`, and lists the `resume`
    subcommand.
  - `plan resume --help` stdout mentions the `--from-task` flag and
    explains that `PLAN_RESUME_FROM_TASK` is the env-var binding.
