Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run
    `./bin/j init --yes --must-read=`.

Steps:
  - Run `./bin/j settings set "project.must_read=AGENTS.md;ClAuDe.MD"`.
  - Run `./bin/j settings`.

Expected:
  - The set invocation exits with code 0.
  - The listing renders, under the `[project]` section header, the
    row `  must_read = AGENTS.md;ClAuDe.MD` EXACTLY — no
    lowercasing, no case folding on the value. The bucket name
    `project` and the settings key `must_read` are also
    case-sensitive: a header spelled `[Project]` or a key spelled
    `Must-Read` is a failure.
