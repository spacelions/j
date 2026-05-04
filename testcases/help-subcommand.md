Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j help plan`.
  - Run `./bin/j help work`.
  - Run `./bin/j help verify`.
  - Run `./bin/j help tasks`.
  - Run `./bin/j help tasks start`.
  - Run `./bin/j help settings`.

Expected:
  - Each invocation exits with code 0.
  - `help plan` stdout begins with `Reads a markdown task description`
    and lists the `resume` subcommand plus the `--from-file`,
    `--tool`, `--model`, `--interactive` flags.
  - `help work` stdout begins with `Resolves a plan to execute` and
    lists the `resume` subcommand plus `--from-file`, `--from-task`,
    `--tool`, `--model`, `--interactive` flags.
  - `help verify` stdout mentions `verifier` and lists the `resume`
    subcommand plus `--from-task`, `--tool`, `--model`, `--interactive`,
    `--max-iterations` flags.
  - `help tasks` stdout mentions the bbolt task log
    (`<cwd>/.j/tasks/list.db`) and lists the `delete` and `enter`
    subcommands.
  - `help tasks start` stdout lists `--from-file` and
    `--plan-requires-approval`; it does NOT list
    `--no-plan-requires-approval`.
  - `help settings` stdout mentions `<cwd>/.j/settings` and lists the
    `reset` and `set` subcommands.
