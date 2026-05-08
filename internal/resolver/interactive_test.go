package resolver

import (
	"strconv"
	"testing"
)

func TestInteractive_ExplicitWins(t *testing.T) {
	if got := Interactive(new(true)); got != true {
		t.Fatalf("Interactive(true) = %v, want true", got)
	}
	if got := Interactive(new(false)); got != false {
		t.Fatalf("Interactive(false) = %v, want false", got)
	}
}

func TestInteractive_NilDefaultsToFalse(t *testing.T) {
	if got := Interactive(nil); got != false {
		t.Fatalf("Interactive(nil) = %v, want false", got)
	}
}

func TestInteractive_BothExplicitCases(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		t.Run(strconv.FormatBool(explicit), func(t *testing.T) {
			if got := Interactive(&explicit); got != explicit {
				t.Errorf("Interactive(%v) = %v, want %v",
					explicit, got, explicit)
			}
		})
	}
}
