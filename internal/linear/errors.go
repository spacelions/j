// Package linear is the Linear-issue task source used by `j plan`
// and `j tasks start`. It carries a thin GraphQL client (GetIssue,
// ListProjects), the markdown formatter that turns an issue into the
// body `j` writes to requirements.md, identifier validation, settings
// wrappers around the per-project bbolt store, and the OpenURL
// browser shim used by the link prompt. The package depends on
// internal/store and the standard library only — no other j-internal
// import — so the source picker, the resolver, and the cli flag paths
// can all consume it without dragging in unrelated UI code.
package linear

import (
	"errors"
	"fmt"
)

// ErrUnauthorized is returned by the client when Linear answers a 401.
// Callers surface it as a single "invalid Linear API key" line so the
// user knows to run `j settings reset linear.api_key` and try again.
var ErrUnauthorized = errors.New("linear: unauthorized (check linear.api_key)")

// ErrNotFound is returned by GetIssue when the GraphQL response
// reports a null issue node. It is wrapped with the requested
// identifier so the user-facing message says "issue ENG-123 not
// found" verbatim.
var ErrNotFound = errors.New("linear: issue not found")

// ErrInvalidIdentifier is returned by ValidateIdentifier when the
// supplied string does not match Linear's `<TEAM>-<NUM>` shape. The
// error wraps the offending value so the caller does not have to
// reformat it.
var ErrInvalidIdentifier = errors.New("linear: invalid identifier (expected pattern like ENG-123)")

// ErrNoAPIKey is returned by the dispatch path when --from-linear is
// supplied but no API key is stored. The message tells the user the
// exact `j settings set` command to run.
var ErrNoAPIKey = errors.New("linear: no API key set; run `j settings set linear.api_key=<lin_api_…>`")

// HTTPError wraps a non-2xx HTTP response from the Linear GraphQL
// endpoint. 401s are mapped to ErrUnauthorized before this type is
// constructed so callers comparing via errors.Is keep working; every
// other status code surfaces as *HTTPError so the cli can print the
// status + body verbatim.
type HTTPError struct {
	Status int
	Body   string
}

// Error renders an *HTTPError as `linear: http <status>: <body>` so
// the cli can print it as a single line.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("linear: http %d: %s", e.Status, e.Body)
}
