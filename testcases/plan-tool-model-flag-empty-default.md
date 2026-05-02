Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`.

Steps:
  - Run `./bin/j plan --help`.
  - Run `./bin/j work --help`.
  - Run `./bin/j verify --help`.

Expected:
  - Each invocation exits with code 0.
  - Every help output contains BOTH `--tool string` and `--model string`
    flag descriptions.
  - Neither flag carries a `(default ...)` suffix — they default to the
    empty string so the surrounding precedence (stored bucket, then
    interactive prompt) keeps working when the user does not pass them.
  - `plan --help` mentions `planner.tool` / `planner.model` and the
    `j settings reset planner.tool` / `j settings reset planner.model`
    re-pick path.
  - `work --help` mentions `worker.tool` / `worker.model` and
    `j settings reset worker.tool` / `j settings reset worker.model`.
  - `verify --help` mentions `verifier.tool` / `verifier.model` and
    `j settings reset verifier.tool` / `j settings reset verifier.model`.
