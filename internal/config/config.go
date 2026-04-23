package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Init configures Viper for this CLI: environment (GOOGLE_API_KEY), optional
// config file, and optional flag override for the API key.
func Init(configFile, googleAPIKeyFlag string) error {
	if err := v.BindEnv("google_api_key", "GOOGLE_API_KEY"); err != nil {
		return fmt.Errorf("config: bind env: %w", err)
	}

	if configFile != "" {
		if _, err := os.Stat(configFile); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("config: file %q does not exist: %w", configFile, err)
			}
			return err
		}
		v.SetConfigFile(configFile)
	}

	if err := v.ReadInConfig(); err != nil {
		if configFile != "" {
			return err
		}
		if !isConfigNotFound(err) {
			return err
		}
	}

	if googleAPIKeyFlag != "" {
		v.Set("google_api_key", googleAPIKeyFlag)
	}
	return nil
}

func isConfigNotFound(err error) bool {
	if err == nil {
		return false
	}
	var n viper.ConfigFileNotFoundError
	return errors.As(err, &n)
}

// GoogleAPIKey returns the configured Gemini / Google API key, or empty
// if unset. Callers should validate before use.
func GoogleAPIKey() string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(v.GetString("google_api_key"))
}

var v = func() *viper.Viper {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.AddConfigPath(".")
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	return v
}()
