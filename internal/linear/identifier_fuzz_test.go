package linear

import (
	"errors"
	"testing"
)

func FuzzValidateIdentifier(f *testing.F) {
	// Seed with representative corpus.
	f.Add("ENG-123")
	f.Add("AB-1")
	f.Add("ABC123-456")
	f.Add("")
	f.Add("abc")
	f.Add("A-0")
	f.Add("ENG-")
	f.Add("-123")
	f.Add("eng-123")
	f.Add("ENG_123")

	f.Fuzz(func(t *testing.T, id string) {
		err := ValidateIdentifier(id)
		if err != nil && !errors.Is(err, ErrInvalidIdentifier) {
			t.Errorf("ValidateIdentifier(%q) returned unexpected error type: %v", id, err)
		}
	})
}
