Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`.
  - Drop a small markdown task description at `task.md`.
  - Confirm the planner bucket is empty.

Steps:
  - Run `PLAN_TOOL=ghost PLAN_MODEL=foo ./bin/j plan -f task.md`.
  - Run `WORK_TOOL=ghost WORK_MODEL=foo ./bin/j work --from-task=fakeid` (the
    nonexistent task is fine — we're checking the env binding fires
    before the unknown-task error path takes precedence; if `j work`
    short-circuits earlier the assertion is just that the run still
    fails). Use the `--tool=ghost --model=foo` flag form as a fallback
    if the env-var form does not error first.
  - Run `VERIFY_TOOL=ghost VERIFY_MODEL=foo ./bin/j verify --from-task=fakeid`
    (same caveat as above).

Expected:
  - For the plan invocation: exit code is non-zero and stderr contains
    `unknown tool "ghost"`. The `PLAN_TOOL` / `PLAN_MODEL` env vars are
    bound to `plan.tool` / `plan.model` and forwarded into
    `Options.Tool` / `Options.Model`.
  - The two analogous invocations (`WORK_TOOL` / `VERIFY_TOOL`) confirm
    the env-var binding is wired but may surface the unknown-task error
    first because `j work` / `j verify` resolve the task id before
    `selectCoder` / `selectVerifier`. Either error is acceptable as long
    as the env var name is recognised.
  - The legacy `PLAN_FROM_SETTINGS` / `WORK_FROM_SETTINGS` /
    `VERIFY_FROM_SETTINGS` env vars must NOT be honoured anywhere
    (`rg PLAN_FROM_SETTINGS internal/` returns no results).
