Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run
    `./bin/j init --yes --mustread=`.

Steps:
  - Run `./bin/j settings set "project.mustread=AGENTS.md;ClAuDe.MD"`.
  - Run `./bin/j settings`.

Expected:
  - The set invocation exits with code 0.
  - The listing contains the row
    `project.mustread = AGENTS.md;ClAuDe.MD` EXACTLY — no
    lowercasing, no case folding. The bucket name `project` and the
    key `mustread` are also case-sensitive: a listing row spelled
    `Project.mustread` or `project.MustRead` is a failure.
