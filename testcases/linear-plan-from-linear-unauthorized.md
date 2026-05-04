Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.
  - Network access to `api.linear.app` is required because the binary
    cannot be redirected to a stub server from the CLI; tests inside
    `internal/linear` (`go test ./internal/linear/...`) cover the
    identical 401 mapping using `httptest.Server` for hermetic builds.
  - Run `./bin/j settings set linear.api_key=lin_api_DEFINITELYBOGUS`
    (any token Linear will reject as unauthorized).

Steps:
  - Run `./bin/j plan --from-linear ENG-123` (token is stored but
    invalid).

Expected:
  - Exit code is non-zero.
  - Output contains a single line
    `J: linear: unauthorized (check linear.api_key)`.
    No HTTP body, no GraphQL boilerplate, no stack trace.
  - No task is created: `./bin/j tasks` reports `J: no tasks`.

Manual: yes (requires outbound HTTPS to api.linear.app). The hermetic
equivalent is `go test ./internal/linear/...`, which exercises the same
401-to-`ErrUnauthorized` mapping against a `httptest.Server`.
