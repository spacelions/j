package tasks

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type flagEnvBinding struct {
	key  string
	flag string
	env  string
}

func bindEnv(key, flag, env string) flagEnvBinding {
	return flagEnvBinding{key: key, flag: flag, env: env}
}

func bindFlagEnv(cmd *cobra.Command, bindings ...flagEnvBinding) {
	flags := cmd.Flags()
	for _, b := range bindings {
		_ = viper.BindPFlag(b.key, flags.Lookup(b.flag))
		_ = viper.BindEnv(b.key, b.env)
	}
}

func explicitBoolPtr(
	cmd *cobra.Command, flag, key, env string,
) *bool {
	if cmd.Flags().Changed(flag) || envSet(env) {
		v := viper.GetBool(key)
		return &v
	}
	return nil
}

func explicitBool(cmd *cobra.Command, flag, key, env string) bool {
	v := explicitBoolPtr(cmd, flag, key, env)
	if v == nil {
		return false
	}
	return *v
}
