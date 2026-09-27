// Package config provides the client's storage and server connection settings.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	ConfigDir    string `mapstructure:"config_dir"`
	DataDir      string `mapstructure:"data_dir"`
	AuthFilePath string `mapstructure:"auth_file_path"`
	AuthServer   Server `mapstructure:"authserver"`
	WorldServer  Server `mapstructure:"worldserver"`
}

var current Config

// Init loads defaults, a config file, and environment overrides. If configFile
// is empty, it looks for an optional file named config in the working directory
// (for example, config.yaml). An explicit configFile must exist. Environment
// variables use the AGENT_WOW prefix, such as AGENT_WOW_AUTHSERVER_HOST and
// AGENT_WOW_WORLDSERVER_PORT. They take precedence over file settings.
// Call Init before starting any other client components.
func Init(configFile string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	v := viper.New()
	v.SetDefault("config_dir", filepath.Join(homeDir, ".config", "agent-wow"))
	v.SetDefault("data_dir", filepath.Join(homeDir, ".local", "share", "agent-wow"))
	v.SetDefault("authserver.host", "localhost")
	v.SetDefault("authserver.port", 3724)
	v.SetDefault("worldserver.host", "localhost")
	v.SetDefault("worldserver.port", 8085)

	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(".")
	}
	v.SetEnvPrefix("AGENT_WOW")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if configFile != "" || !errors.As(err, &notFound) {
			return fmt.Errorf("read config: %w", err)
		}
	}

	// Resolve this default after loading overrides so it follows ConfigDir.
	v.SetDefault("auth_file_path", filepath.Join(v.GetString("config_dir"), "auth.json"))
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	if strings.TrimSpace(cfg.ConfigDir) == "" {
		return errors.New("config_dir must not be empty")
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		return errors.New("data_dir must not be empty")
	}
	if strings.TrimSpace(cfg.AuthFilePath) == "" {
		return errors.New("auth_file_path must not be empty")
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
