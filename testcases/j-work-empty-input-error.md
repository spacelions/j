Prerequisites:
  - Repo at HEAD with `internal/cli/work/ui.go` containing the `errEmptyFromFile` sentinel.

Steps:
  - Open `internal/cli/work/ui.go`. Confirm a package-private sentinel
    `var errEmptyFromFile = errors.New("J: no markdown provided")` exists.
  - Confirm `huhUI.AskFromFile` returns `errEmptyFromFile` when the
    trimmed input value is empty (no inline literal).
  - Run `go test ./internal/cli/work/ -run TestErrEmptyFromFile_Message -v`.
    Expect PASS; the test asserts `errEmptyFromFile.Error() == "J: no markdown provided"`.

Acceptance:
  - `errEmptyFromFile.Error()` is exactly `J: no markdown provided` (AC#1).
  - The sentinel is package-private, no test seam was introduced (AGENTS rule).
