package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoragePaths(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	defaultConfigDir := filepath.Join(homeDir, ".config", "agent-wow")
	defaultDataDir := filepath.Join(homeDir, ".local", "share", "agent-wow")
	for _, tc := range []struct {
		name, config, envConfigDir, envDataDir, envFile, wantConfigDir, wantDataDir, wantFile, wantErr string
	}{
		{name: "defaults", config: `{}`, wantConfigDir: defaultConfigDir, wantDataDir: defaultDataDir, wantFile: filepath.Join(defaultConfigDir, "auth.json")},
		{name: "configured config directory", config: `{"config_dir":"/tmp/my-client"}`, wantConfigDir: "/tmp/my-client", wantDataDir: defaultDataDir, wantFile: "/tmp/my-client/auth.json"},
		{name: "separate data directory", config: `{"data_dir":"/tmp/db"}`, wantConfigDir: defaultConfigDir, wantDataDir: "/tmp/db", wantFile: filepath.Join(defaultConfigDir, "auth.json")},
		{name: "explicit auth file", config: `{"config_dir":"/tmp/my-client","auth_file_path":"/tmp/credentials.json"}`, wantConfigDir: "/tmp/my-client", wantDataDir: defaultDataDir, wantFile: "/tmp/credentials.json"},
		{name: "config directory environment override", config: `{"config_dir":"/tmp/file-dir"}`, envConfigDir: "/tmp/env-dir", wantConfigDir: "/tmp/env-dir", wantDataDir: defaultDataDir, wantFile: "/tmp/env-dir/auth.json"},
		{name: "data directory environment override", config: `{"data_dir":"/tmp/file-dir"}`, envDataDir: "/tmp/env-dir", wantConfigDir: defaultConfigDir, wantDataDir: "/tmp/env-dir", wantFile: filepath.Join(defaultConfigDir, "auth.json")},
		{name: "file environment override", config: `{"auth_file_path":"/tmp/from-config.json"}`, envFile: "/tmp/from-env.json", wantConfigDir: defaultConfigDir, wantDataDir: defaultDataDir, wantFile: "/tmp/from-env.json"},
		{name: "dev directories", config: `{"config_dir":"./config","data_dir":"./data"}`, wantConfigDir: "./config", wantDataDir: "./data", wantFile: "config/auth.json"},
		{name: "empty config directory", config: `{"config_dir":""}`, wantErr: "config_dir must not be empty"},
		{name: "empty data directory", config: `{"data_dir":""}`, wantErr: "data_dir must not be empty"},
		{name: "empty auth file", config: `{"auth_file_path":""}`, wantErr: "auth_file_path must not be empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENT_WOW_CONFIG_DIR", tc.envConfigDir)
			t.Setenv("AGENT_WOW_DATA_DIR", tc.envDataDir)
			t.Setenv("AGENT_WOW_AUTH_FILE_PATH", tc.envFile)
			t.Setenv("AGENT_WOW_REALM_FILE_PATH", "")
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			err := Init(path)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg := Get()
			if cfg.ConfigDir != tc.wantConfigDir || cfg.DataDir != tc.wantDataDir || cfg.AuthFilePath != tc.wantFile {
				t.Errorf("unexpected storage paths: ConfigDir=%q, DataDir=%q, AuthFilePath=%q", cfg.ConfigDir, cfg.DataDir, cfg.AuthFilePath)
			}
			if want := filepath.Join(tc.wantConfigDir, "realm.json"); cfg.RealmFilePath != want {
				t.Errorf("RealmFilePath=%q, want %q", cfg.RealmFilePath, want)
			}
		})
	}
}

func TestRealmFilePath(t *testing.T) {
	for _, tc := range []struct {
		name, config, envFile, want, wantErr string
	}{
		{name: "explicit file", config: `{"realm_file_path":"/tmp/selected-realm.json"}`, want: "/tmp/selected-realm.json"},
		{name: "environment override", config: `{"realm_file_path":"/tmp/from-config.json"}`, envFile: "/tmp/from-env.json", want: "/tmp/from-env.json"},
		{name: "empty file", config: `{"realm_file_path":""}`, wantErr: "realm_file_path must not be empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENT_WOW_REALM_FILE_PATH", tc.envFile)
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			err := Init(path)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := Get().RealmFilePath; got != tc.want {
				t.Errorf("RealmFilePath=%q, want %q", got, tc.want)
			}
		})
	}
}
