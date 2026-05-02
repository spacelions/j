Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run
    `./bin/j init --yes --must-read=`.

Steps:
  - Run `./bin/j settings set "project.mustread=AGENTS.md;ClAuDe.MD"`.
  - Run `./bin/j settings`.

Expected:
  - The set invocation exits with code 0.
  - The listing renders, under the `[project]` section header, the
    row `  mustread = AGENTS.md;ClAuDe.MD` EXACTLY — no
    lowercasing, no case folding. The bucket name `project` and the
    key `mustread` are also case-sensitive: a header spelled
    `[Project]` or a key spelled `MustRead` is a failure.
