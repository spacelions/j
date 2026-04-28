package strs

import (
	"errors"
	"reflect"
	"testing"
)

func TestTrimListPrefix(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"x":           "x",
		"- x":         "x",
		"* x":         "x",
		"• x":         "x",
		"1. x":        "x",
		"23) x":       "x",
		"  1.  x  ":   "x",
		"not a list.": "not a list.",
	}
	for in, want := range cases {
		if got := TrimListPrefix(in); got != want {
			t.Fatalf("TrimListPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsAllDigits(t *testing.T) {
	cases := map[string]bool{
		"":     true,
		"0":    true,
		"123":  true,
		"12a":  false,
		"abc":  false,
		" 12":  false,
		"12 3": false,
	}
	for in, want := range cases {
		if got := isAllDigits(in); got != want {
			t.Fatalf("isAllDigits(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseList(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		noise []string
		want  []string
	}{
		{"plain", "gpt-5\nsonnet-4\n", nil, []string{"gpt-5", "sonnet-4"}},
		{"trims", "  gpt-5  \n\n\tsonnet-4\n", nil, []string{"gpt-5", "sonnet-4"}},
		{"bullets", "- gpt-5\n* sonnet-4\n1. claude-3.5\n2) opus-4\n", nil, []string{"gpt-5", "sonnet-4", "claude-3.5", "opus-4"}},
		{"noise-skipped", "No models available for this account.\ngpt-5\n", []string{"no models"}, []string{"gpt-5"}},
		{"noise-empty-string-ignored", "gpt-5\n", []string{""}, []string{"gpt-5"}},
		{"number-only-line-skipped", "1.\ngpt-5\n", nil, []string{"gpt-5"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseList(tc.in, tc.noise...)
			if err != nil {
				t.Fatalf("ParseList: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseList_EmptyList(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		noise []string
	}{
		{"all-blank", "\n\n  \n", nil},
		{"all-noise", "No models available for this account.\n", []string{"no models"}},
		{"all-numeric-prefix-only", "1.\n2)\n", nil},
		{"empty-input", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseList(tc.in, tc.noise...)
			if !errors.Is(err, ErrEmptyList) {
				t.Fatalf("err = %v, want ErrEmptyList", err)
			}
		})
	}
}
