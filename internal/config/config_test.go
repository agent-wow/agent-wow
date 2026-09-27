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
	defaultDir := filepath.Join(homeDir, ".config", "agent-wow")
	for _, tc := range []struct {
		name, config, envDir, envFile, wantDir, wantFile, wantErr string
	}{
		{name: "defaults", config: `{}`, wantDir: defaultDir, wantFile: filepath.Join(defaultDir, "auth.json")},
		{name: "configured directory", config: `{"data_dir":"/tmp/my-client"}`, wantDir: "/tmp/my-client", wantFile: "/tmp/my-client/auth.json"},
		{name: "explicit auth file", config: `{"data_dir":"/tmp/my-client","auth_file_path":"/tmp/credentials.json"}`, wantDir: "/tmp/my-client", wantFile: "/tmp/credentials.json"},
		{name: "directory environment override", config: `{"data_dir":"/tmp/file-dir"}`, envDir: "/tmp/env-dir", wantDir: "/tmp/env-dir", wantFile: "/tmp/env-dir/auth.json"},
		{name: "file environment override", config: `{"auth_file_path":"/tmp/from-config.json"}`, envFile: "/tmp/from-env.json", wantDir: defaultDir, wantFile: "/tmp/from-env.json"},
		{name: "empty directory", config: `{"data_dir":""}`, wantErr: "data_dir must not be empty"},
		{name: "empty auth file", config: `{"auth_file_path":""}`, wantErr: "auth_file_path must not be empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENT_WOW_DATA_DIR", tc.envDir)
			t.Setenv("AGENT_WOW_AUTH_FILE_PATH", tc.envFile)
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
			if cfg.DataDir != tc.wantDir || cfg.AuthFilePath != tc.wantFile {
				t.Errorf("unexpected storage paths: DataDir=%q, AuthFilePath=%q", cfg.DataDir, cfg.AuthFilePath)
			}
		})
	}
}
