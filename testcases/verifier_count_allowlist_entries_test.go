package testcases_test

import (
	"testing"
)

// TestCountAllowlistEntries_SkipsBlankAndComment verifies the counting
// helper ignores blank lines and lines starting with '#', so the
// regression guard ceiling reflects only real exemption patterns.
func TestCountAllowlistEntries_SkipsBlankAndComment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "empty",
			input: "",
			want:  0,
		},
		{
			name:  "blank lines only",
			input: "\n\n\n",
			want:  0,
		},
		{
			name:  "comment only",
			input: "# this is a comment\n",
			want:  0,
		},
		{
			name:  "one real entry",
			input: "internal/foo/bar.go:.*Baz\n",
			want:  1,
		},
		{
			name: "mixed: comments blank and entries",
			input: "# header\n\ninternal/a.go:.*A\n" +
				"# another comment\ninternal/b.go:.*B\n\n",
			want: 2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := countAllowlistEntries(c.input); got != c.want {
				t.Fatalf(
					"countAllowlistEntries = %d, want %d",
					got, c.want,
				)
			}
		})
	}
}
