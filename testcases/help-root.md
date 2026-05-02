Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j help` (and separately `./bin/j --help`).

Expected:
  - Exit code 0.
  - Stdout begins with `J Harness CLI`.
  - Stdout lists every top-level subcommand: `completion`, `help`, `init`,
    `plan`, `run`, `settings`, `tasks`, `verify`, `web`, `work`.
  - Stdout ends with the line
    `Use "j [command] --help" for more information about a command.`
