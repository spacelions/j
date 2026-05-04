Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `PLAN_FROM_LINEAR=foo ./bin/j plan` (no `--from-linear` flag,
    no token stored).

Expected:
  - Exit code is non-zero.
  - Output contains a single line
    `J: linear: invalid identifier (expected pattern like ENG-123): "foo"`.
    The presence of this error confirms two things:
      (a) the `PLAN_FROM_LINEAR` env-var binding is wired (the dispatch
          short-circuits the source picker), and
      (b) identifier validation runs before the API-key check (so the
          first error the user sees is the structural one).
  - No task is created: `./bin/j tasks` reports `J: no tasks`.
