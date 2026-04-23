package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Sentinel errors returned by Load when required environment variables are
// unset. Set them via the environment directly or via direnv / .envrc.
var (
	ErrMissingAPIKey        = errors.New("GOOGLE_API_KEY is not set")
	ErrMissingModel         = errors.New("MODEL is not set")
	ErrMissingMaxIterations = errors.New("MAX_ITERATIONS is not set or is 0")
)

// Config bundles the runtime knobs consumed by the workflow. It lives in the
// config package so the workflow package does not depend on Viper or on how
// values are sourced.
type Config struct {
	// APIKey is the Gemini / Google API key.
	APIKey string
	// Model is the Gemini model name (e.g. "gemini-2.5-flash").
	Model string
	// MaxIterations bounds the coder/verifier loop.
	MaxIterations uint
}

// Load returns a populated Config from the current Viper state. Call Init
// first. Returns a sentinel error (ErrMissingAPIKey, ErrMissingModel, or
// ErrMissingMaxIterations) when any required env var is unset.
func Load() (Config, error) {
	cfg := Config{
		APIKey:        GoogleAPIKey(),
		Model:         Model(),
		MaxIterations: MaxIterations(),
	}
	if cfg.APIKey == "" {
		return cfg, ErrMissingAPIKey
	}
	if cfg.Model == "" {
		return cfg, ErrMissingModel
	}
	if cfg.MaxIterations == 0 {
		return cfg, ErrMissingMaxIterations
	}
	return cfg, nil
}

// Init configures the global Viper singleton for this CLI: it binds the
// environment variables GOOGLE_API_KEY, MODEL, and MAX_ITERATIONS.
func Init() error {
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.BindEnv("google_api_key", "GOOGLE_API_KEY"); err != nil {
		return fmt.Errorf("config: bind env: %w", err)
	}
	if err := viper.BindEnv("model", "MODEL"); err != nil {
		return fmt.Errorf("config: bind env: %w", err)
	}
	if err := viper.BindEnv("max_iterations", "MAX_ITERATIONS"); err != nil {
		return fmt.Errorf("config: bind env: %w", err)
	}
	return nil
}

// GoogleAPIKey returns the configured Gemini / Google API key, or empty if
// unset.
func GoogleAPIKey() string {
	return strings.TrimSpace(viper.GetString("google_api_key"))
}

// Model returns the raw MODEL env value, or empty if unset.
func Model() string {
	return strings.TrimSpace(viper.GetString("model"))
}

// MaxIterations returns the raw MAX_ITERATIONS env value, clamped to >= 0, or
// 0 if unset.
func MaxIterations() uint {
	n := viper.GetInt("max_iterations")
	if n < 0 {
		n = 0
	}
	return uint(n)
}
