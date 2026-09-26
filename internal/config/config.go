// Package config provides the client's server connection settings.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Server contains the host and TCP port of a server.
type Server struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// Config contains the settings shared by client packages.
type Config struct {
	AuthServer  Server `mapstructure:"authserver"`
	WorldServer Server `mapstructure:"worldserver"`
}

var current Config

// Init loads defaults, an optional config file named config in the working
// directory (for example, config.yaml), and environment overrides. Environment
// variables use the AGENT_WOW prefix, such as AGENT_WOW_AUTHSERVER_HOST and
// AGENT_WOW_WORLDSERVER_PORT. They take precedence over file settings.
// Call Init from main before starting any other client components.
func Init() error {
	v := viper.New()
	v.SetDefault("authserver.host", "localhost")
	v.SetDefault("authserver.port", 3724)
	v.SetDefault("worldserver.host", "localhost")
	v.SetDefault("worldserver.port", 8085)

	v.SetConfigName("config")
	v.AddConfigPath(".")
	v.SetEnvPrefix("AGENT_WOW")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	if err := validateServer("authserver", cfg.AuthServer); err != nil {
		return err
	}
	if err := validateServer("worldserver", cfg.WorldServer); err != nil {
		return err
	}
	current = cfg
	return nil
}

// Get returns a copy of the configuration loaded by Init.
func Get() Config {
	return current
}

func validateServer(name string, server Server) error {
	if strings.TrimSpace(server.Host) == "" {
		return fmt.Errorf("%s.host must not be empty", name)
	}
	if server.Port < 1 || server.Port > 65535 {
		return fmt.Errorf("%s.port must be between 1 and 65535", name)
	}
	return nil
}
