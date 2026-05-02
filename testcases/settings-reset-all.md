Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps (headless variant — runnable):
  - Run `./bin/j settings set plan.tool=cursor`.
  - Run `./bin/j settings set plan.model=sonnet-4`.
  - Run `./bin/j settings reset --yes`.
  - Confirm `.j/` no longer exists on disk: `test ! -d .j && echo gone`.

Expected (headless):
  - `reset --yes` exits with code 0 and stdout contains
    `removed <abs-path-to-.j>` (the entire `.j/` directory is wiped,
    not just the settings DB — see `internal/cli/settings/reset.go`
    `runResetFull` calling `os.RemoveAll(jDir)`).
  - After `reset --yes` returns, `<cwd>/.j/` does NOT exist.

Steps (interactive variant — MANUAL, requires a TTY):
  - Run `./bin/j init --yes` to recreate the layout.
  - Run `./bin/j settings set plan.tool=cursor`.
  - Run `./bin/j settings reset` WITHOUT `--yes`. The confirmation
    prompt appears on stdin (line-based: type `y` and Enter to accept,
    anything else to abort).
    - Type `n` and Enter: exit 0, stdout reports `reset aborted`,
      `.j/` still exists, `j settings` still shows `plan.tool=cursor`.
    - Re-init, set the key again, run `reset` again, this time type
      `y` and Enter: exit 0, stdout reports `removed <abs-path>`,
      `.j/` is gone.

Manual: yes (the second variant only — needs interactive stdin).
