package config

import (
	"errors"
	"testing"

	"github.com/spf13/viper"
)

// resetViper wipes the global Viper singleton so tests don't leak into each
// other.
func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
}

func TestInit_Succeeds(t *testing.T) {
	resetViper(t)
	t.Setenv("GOOGLE_API_KEY", "")

	if err := Init(); err != nil {
		t.Fatalf("Init error: %v", err)
	}
}

func TestGettersDefaults(t *testing.T) {
	resetViper(t)
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("MODEL", "")
	t.Setenv("MAX_ITERATIONS", "")

	if err := Init(); err != nil {
		t.Fatal(err)
	}
	if got := Model(); got != "gemini-2.5-flash" {
		t.Fatalf("default Model = %q", got)
	}
	if got := MaxIterations(); got != 3 {
		t.Fatalf("default MaxIterations = %d", got)
	}
	if got := GoogleAPIKey(); got != "" {
		t.Fatalf("expected empty key, got %q", got)
	}
}

func TestGettersEnvOverrideAndTrim(t *testing.T) {
	resetViper(t)
	t.Setenv("GOOGLE_API_KEY", "  spaced-key  ")
	t.Setenv("MODEL", "  my-model  ")
	t.Setenv("MAX_ITERATIONS", "9")

	if err := Init(); err != nil {
		t.Fatal(err)
	}
	if got := GoogleAPIKey(); got != "spaced-key" {
		t.Fatalf("GoogleAPIKey = %q", got)
	}
	if got := Model(); got != "my-model" {
		t.Fatalf("Model = %q", got)
	}
	if got := MaxIterations(); got != 9 {
		t.Fatalf("MaxIterations = %d", got)
	}
}

func TestLoad_Success(t *testing.T) {
	resetViper(t)
	t.Setenv("GOOGLE_API_KEY", "k")
	t.Setenv("MODEL", "m")
	t.Setenv("MAX_ITERATIONS", "2")

	if err := Init(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	want := Config{APIKey: "k", Model: "m", MaxIterations: 2}
	if got != want {
		t.Fatalf("Load = %+v, want %+v", got, want)
	}
}

func TestLoad_MissingAPIKey(t *testing.T) {
	resetViper(t)
	t.Setenv("GOOGLE_API_KEY", "")

	if err := Init(); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("Load err = %v, want ErrMissingAPIKey", err)
	}
}

func TestMaxIterationsNegativeClamp(t *testing.T) {
	resetViper(t)
	t.Setenv("MAX_ITERATIONS", "-5")

	if err := Init(); err != nil {
		t.Fatal(err)
	}
	if got := MaxIterations(); got != 0 {
		t.Fatalf("expected clamp to 0, got %d", got)
	}
}
