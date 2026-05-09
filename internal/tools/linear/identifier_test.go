package linear

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateIdentifier(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"two-char prefix", "EN-1", false},
		{"three-char prefix", "ENG-123", false},
		{"alphanumeric prefix", "X1-7", false},
		{"single-letter prefix", "E-1", true},
		{"lowercase prefix", "eng-1", true},
		{"missing dash", "ENG1", true},
		{"all numeric prefix", "123-7", true},
		{"empty", "", true},
		{"trailing whitespace", "ENG-1 ", true},
		{"missing number", "ENG-", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateIdentifier(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateIdentifier(%q) err = %v, wantErr=%v", tc.input, err, tc.wantErr)
			}
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidIdentifier) {
					t.Fatalf("err = %v, want errors.Is ErrInvalidIdentifier", err)
				}
				if !strings.Contains(err.Error(), tc.input) && tc.input != "" {
					t.Fatalf("err = %q, want to mention input %q", err, tc.input)
				}
			}
		})
	}
}
