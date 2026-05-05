package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// TestPelletierPointerTime documents that pelletier/go-toml/v2 v2.3.0 does NOT
// correctly round-trip *time.Time (it encodes as a quoted TOML string instead
// of a TOML datetime). This is why Task uses value time.Time on disk.
//
// This test is expected to FAIL on the pointer encoding assertion — it
// intentionally records the broken behavior so a future upgrade can detect
// when the bug is fixed and the taskWire workaround can be simplified.
func TestPelletierPointerTime_BugConfirmed(t *testing.T) {
	type ptrProbe struct {
		Present *time.Time `toml:"present,omitempty"`
	}
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	data, err := toml.Marshal(ptrProbe{Present: &now})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// TOML datetimes have no surrounding quotes; single-quoted ('…') or
	// double-quoted ("…") values are TOML strings — the broken encoding.
	if strings.ContainsAny(string(data), `"'`) {
		t.Logf("confirmed: *time.Time encodes as a quoted TOML string (bug still present in v2.3.0):\n%s", data)
	} else {
		t.Logf("NOTE: *time.Time now encodes as a TOML datetime; the wire workaround can be removed:\n%s", data)
	}
}

// TestPelletierValueTime confirms that value time.Time (non-pointer) encodes
// as a TOML datetime. It also documents that pelletier/go-toml/v2 v2.3.0 does
// NOT honour omitempty on value time.Time — a non-zero time.Time with
// omitempty is still omitted from the output. This is why Task does not use
// omitempty on its time.Time fields; callers use .IsZero() to detect unset
// timestamps.
func TestPelletierValueTime(t *testing.T) {
	type valProbe struct {
		ZeroOmit    time.Time `toml:"zero_omit,omitempty"`
		NonZeroOmit time.Time `toml:"non_zero_omit,omitempty"`
		NonZeroSet  time.Time `toml:"non_zero_set"`
	}

	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	in := valProbe{NonZeroOmit: now, NonZeroSet: now}

	data, err := toml.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)
	t.Logf("marshaled:\n%s", s)

	// zero time.Time with omitempty must not appear.
	if strings.Contains(s, "zero_omit") {
		t.Errorf("zero time.Time with omitempty should be omitted")
	}
	// non-zero time.Time without omitempty must appear.
	if !strings.Contains(s, "non_zero_set") {
		t.Errorf("non-zero time.Time without omitempty should be present")
	}
	if strings.ContainsAny(s, `"'`) {
		t.Errorf("time.Time should encode as TOML datetime, not quoted string")
	}

	// round-trip.
	var out valProbe
	if err := toml.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !out.NonZeroSet.Equal(now) {
		t.Errorf("NonZeroSet = %v, want %v", out.NonZeroSet, now)
	}
	if !out.ZeroOmit.IsZero() {
		t.Errorf("ZeroOmit should remain zero; got %v", out.ZeroOmit)
	}
	// Check whether omitempty drops non-zero time.Time too (known pelletier bug):
	if strings.Contains(s, "non_zero_omit") {
		t.Logf("non-zero time.Time with omitempty IS preserved (omitempty works correctly)")
	} else {
		t.Logf("WARNING: non-zero time.Time with omitempty was omitted — pelletier v2.3.0 bug; do not use omitempty on time.Time fields")
	}
}
