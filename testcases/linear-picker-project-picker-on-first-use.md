Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.
  - Pre-populate every agent bucket so `EnsureAgentSelections` does
    not block on a TTY prompt:
      ./bin/j settings set planner.tool=cursor planner.model=auto \
                            worker.tool=cursor worker.model=auto \
                            verifier.tool=cursor verifier.model=auto
  - Store a real Linear API key whose owning workspace has at least
    one project visible:
      ./bin/j settings set linear.api_key=lin_api_…
  - Confirm `./bin/j settings` does NOT yet render a
    `project = …` row under `[linear]` (i.e. the project key is
    absent).

Steps:
  - In a TTY, run `./bin/j tasks start` and select `linear` at the
    source picker.

Expected:
  - The browser does NOT open (the API key is already stored).
  - The first interactive prompt is the project picker: a
    single-select widget titled `Select default Linear project`,
    populated by `client.ListProjects()` (one row per project the
    API key can see).
  - Choosing one project advances to the assigned-issues picker;
    aborting (Ctrl-C / Esc) exits the command cleanly with no task
    created.
  - After a successful selection, `./bin/j settings` renders the
    `project = <id>` row under `[linear]`. The id is the Linear
    GraphQL node id of the chosen project (not its name).
  - A second `./bin/j tasks start` (still in the same project)
    skips the project picker entirely and goes straight to the
    issue picker — the persisted `linear.project` short-circuits
    the project prompt.
  - When the API key has zero visible projects, the picker is
    skipped silently (no prompt fires) and `linear.project`
    remains absent in `./bin/j settings`. The flow falls through to
    the issue picker.
