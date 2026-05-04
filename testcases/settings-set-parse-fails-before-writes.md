Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set a.b=1 bad-no-equals c.d=2`. Capture the exit
    code.
  - Run `./bin/j settings`.

Expected:
  - The first command exits non-zero and prints an error mentioning
    `"bad-no-equals"` and `missing '='`.
  - The `j settings` listing renders the four known sections, with
    `[project]` carrying the seeded `  must_read = ` and
    `  plan_requires_approval = true` rows and the
    other three sections empty:

        [project]
          must_read = 
          plan_requires_approval = true
        
        [planner]
        
        [worker]
        
        [verifier]

  - Neither `[a]` nor `[c]` appears anywhere in the listing,
    confirming the batch aborted before any `Put` ran.
