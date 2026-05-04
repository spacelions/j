Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set linear.api_key=lin_api_TESTTOKEN`.
  - Run `./bin/j settings`.

Expected:
  - The `set` command exits with code 0 and prints
    `J: set linear.api_key = lin_api_TESTTOKEN` (or equivalent
    confirmation; exit 0 is load-bearing).
  - The follow-up `j settings` listing renders a `[linear]` section
    appended after the four known sections, containing exactly:

        [linear]
          api_key = ****

  - The token must NEVER appear verbatim in `j settings` output:
    the masked `****` value confirms the secret-key allowlist
    handles the Linear API key.
