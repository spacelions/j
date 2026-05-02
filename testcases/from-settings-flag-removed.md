Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`.

Steps:
  - Run `./bin/j plan --from-settings`.
  - Run `./bin/j work --from-settings`.
  - Run `./bin/j verify --from-settings`.

Expected:
  - Each invocation exits with non-zero status (cobra unknown flag).
  - Each stderr/stdout contains the literal `unknown flag: --from-settings`.
  - The flag is removed from every role command per the new precedence
    (re-pick is `j settings reset <role>.tool` / `<role>.model`).
