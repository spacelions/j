Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`.

Steps:
  - Run `./bin/j settings set coder.tool=cursor coder.model=gpt-5 coder.interactive=false`.
  - Run `./bin/j settings`.

Expected:
  - Exit code 0.
  - Inside `[coder]`, keys appear in strict alphabetical order regardless
    of the insertion order on the `set` command line:

    [coder]
      interactive = false
      model = gpt-5
      tool = cursor

  - The other known sections render in fixed order with no entries
    (except `[project]` which still carries the seeded `mustread = `).
