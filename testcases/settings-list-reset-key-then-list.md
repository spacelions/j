Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.

Steps:
  - Run `./bin/j settings set planner.tool=cursor`.
  - Run `./bin/j settings set planner.model=sonnet-4`.
  - Run `./bin/j settings reset planner.tool`.
  - Run `./bin/j settings`.

Expected:
  - All `set` and `reset` commands exit 0.
  - The final listing still contains the four known sections in order
    and `[planner]` retains only `model = sonnet-4`:

    [project]
      mustread = 
    
    [planner]
      model = sonnet-4
    
    [coder]
    
    [verifier]

  - This pins the requirement that `set`/`reset` semantics are NOT
    modified by the rendering change: a single-key reset still leaves
    the rest of the bucket intact, and the listing reflects it.
