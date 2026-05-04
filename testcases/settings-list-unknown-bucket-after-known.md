Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.

Steps:
  - Run `./bin/j settings set zeta.k=v`.
  - Run `./bin/j settings set alpha.x=y`.
  - Run `./bin/j settings`.

Expected:
  - Exit code 0.
  - The four known sections appear FIRST, in fixed order (`[project]`,
    `[planner]`, `[worker]`, `[verifier]`).
  - The two unknown buckets appear AFTER `[verifier]`, in alphabetical
    order: `[alpha]` before `[zeta]`.
  - Stdout (modulo the seeded `[project]` rows) is:

    [project]
      must_read = 
      plan_requires_approval = true
    
    [planner]
    
    [worker]
    
    [verifier]
    
    [alpha]
      x = y
    
    [zeta]
      k = v

  - Each unknown section uses the same indent / `key = value` format.
  - No trailing blank line after `[zeta]`.
