Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set linear.project=my-default-project`.
  - Run `./bin/j settings`.

Expected:
  - The `set` command exits with code 0.
  - The follow-up `j settings` listing renders the `[linear]` section
    containing the row `  project = my-default-project`. The project id
    is NOT masked (only the API key is a secret).
