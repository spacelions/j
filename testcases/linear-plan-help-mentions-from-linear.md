Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j plan --help`.

Expected:
  - Exit code 0.
  - Stdout contains a `--from-linear string` flag whose description
    mentions `Linear issue identifier`, the example `ENG-123`, and
    requires the `linear.api_key` setting.
  - Stdout still mentions the existing `--from-file` (`-f`),
    `--from-task`, `--tool`, `--model`, and `--interactive` flags so
    the new flag does not displace them.
