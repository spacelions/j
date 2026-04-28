package cursor

import (
	"reflect"
	"testing"
)

const sampleListModels = `Available models

auto - Auto
composer-2-fast - Composer 2 Fast (default)
composer-2 - Composer 2
gpt-5.3-codex-low - Codex 5.3 Low
`

func TestAgent_Name(t *testing.T) {
	if got := New().Name(); got != "cursor" {
		t.Fatalf("Name = %q, want %q", got, "cursor")
	}
}

func TestParseModels(t *testing.T) {
	got := parseModels(sampleListModels)
	want := []string{"auto", "composer-2-fast", "composer-2", "gpt-5.3-codex-low"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseModels_SkipsHeaderAndBlanks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"banner-only", "Available models\n", nil},
		{"all-blank", "\n\n  \n", nil},
		{"empty", "", nil},
		{"separator-without-id", " - Description\n", nil},
		{"trailing-blanks", "auto - Auto\n\n", []string{"auto"}},
		{"mixed", "Available models\n\nauto - Auto\nsome banner line\nfoo-bar - Foo Bar\n", []string{"auto", "foo-bar"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseModels(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
