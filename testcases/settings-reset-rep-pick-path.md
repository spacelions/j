Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`.

Steps:
  - Run `./bin/j settings set planner.tool=cursor planner.model=sonnet-4`.
  - Run `./bin/j settings`. Confirm both rows appear.
  - Run `./bin/j settings reset planner.tool`.
  - Run `./bin/j settings`. Confirm only `planner.model = sonnet-4`
    remains.
  - Run `./bin/j settings reset planner.model`.
  - Run `./bin/j settings`. Confirm neither `planner.tool` nor
    `planner.model` rows are listed.

Expected:
  - Each step exits with code 0.
  - The keys removed by `reset planner.tool` / `reset planner.model`
    cause the next `j plan` invocation to land in the interactive
    `agentpick.Pick` path (verified by inspection of selectPlanner in
    `internal/cli/plan/plan.go`). This is the documented re-pick path
    that replaces the deleted `--from-settings=false` toggle.
