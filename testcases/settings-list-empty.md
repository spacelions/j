Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings` (no subcommand) on the freshly initialised
    project.

Expected:
  - Exit code 0.
  - Stdout renders the four known sections in TOML order, with the only
    seeded row (`must-read = `) under `[project]`:

        [project]
          must-read = 
        
        [planner]
        
        [worker]
        
        [verifier]

  - There is exactly one blank line between sections and NO trailing
    blank line after `[verifier]`.
  - The "no settings stored" branch is unreachable from a cobra-driven
    init now that preflight always persists at least one row.
