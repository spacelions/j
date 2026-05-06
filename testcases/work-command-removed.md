Prerequisites:
  - Run `go build -o /tmp/j-test-bin ./cmd/j` in the worktree.
  - Confirm the binary exists with `test -x /tmp/j-test-bin && echo ok`.

Steps:
  - Run `/tmp/j-test-bin work --help`.
  - Run `/tmp/j-test-bin work resume --help`.

Expected:
  - Both invocations exit with non-zero code.
  - Both invocations stderr contain `unknown command "work" for "j"`.
