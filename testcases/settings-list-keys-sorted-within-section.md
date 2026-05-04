Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.

Steps:
  - Run `./bin/j settings set worker.tool=cursor worker.model=gpt-5 worker.interactive=false`.
  - Run `./bin/j settings`.

Expected:
  - Exit code 0.
  - Inside `[worker]`, keys appear in strict alphabetical order regardless
    of the insertion order on the `set` command line:

    [worker]
      interactive = false
      model = gpt-5
      tool = cursor

  - The other known sections render in fixed order with no entries
    (except `[project]` which still carries the seeded project rows).
