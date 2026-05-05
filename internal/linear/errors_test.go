package linear

import (
	"errors"
	"strings"
	"testing"
)

func TestHTTPError_Error(t *testing.T) {
	err := &HTTPError{Status: 500, Body: "boom"}
	if !strings.Contains(err.Error(), "http 500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Error() = %q, want status + body", err.Error())
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	// Each sentinel must be distinguishable via errors.Is so callers
	// can map them to user-facing messages without scraping strings.
	if errors.Is(ErrUnauthorized, ErrNotFound) {
		t.Fatal("ErrUnauthorized should not match ErrNotFound")
	}
	if errors.Is(ErrInvalidIdentifier, ErrNoAPIKey) {
		t.Fatal("ErrInvalidIdentifier should not match ErrNoAPIKey")
	}
}
