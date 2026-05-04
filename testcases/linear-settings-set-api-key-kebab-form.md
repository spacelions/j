Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set linear.api-key=lin_api_KEBAB`.
  - Run `./bin/j settings`.

Expected:
  - The `set` command exits with code 0.
  - The follow-up `j settings` listing renders the `[linear]` section
    with the row `  api_key = ****` — i.e. both
    `linear.api_key=…` and `linear.api-key=…` round-trip to the same
    bbolt key (`apiKey`) and surface as the kebab-display form
    `api_key`.
