// Package credentials manages the client's local auth.json file.
package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Credentials is the on-disk auth.json format. Password is stored in plaintext;
// callers must not log Credentials or include it in command output.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (c Credentials) validate() error {
	if len(c.Username) == 0 || len(c.Username) > 17 || !utf8.ValidString(c.Username) || strings.ContainsAny(c.Username, "\x00\r\n") {
		return errors.New("username must contain 1 to 17 UTF-8 bytes without NUL or newlines")
	}
	if c.Password == "" || !utf8.ValidString(c.Password) {
		return errors.New("password must be nonempty UTF-8")
	}
	return nil
}

// Read loads and validates an auth file without including its secret
// contents in errors. A missing file can be detected with errors.Is(err, os.ErrNotExist).
func Read(path string) (Credentials, error) {
	var credentials Credentials
	file, err := os.Open(path)
	if err != nil {
		return credentials, fmt.Errorf("read auth file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return credentials, fmt.Errorf("read auth file %q: %w", path, err)
	}
	if len(data) > 65536 {
		return credentials, fmt.Errorf("auth file %q is too large", path)
	}
	if err := json.Unmarshal(data, &credentials); err != nil {
		return Credentials{}, fmt.Errorf("invalid auth file %q: expected a JSON object with string username and password fields", path)
	}
	if err := credentials.validate(); err != nil {
		return Credentials{}, fmt.Errorf("invalid auth file %q: %w", path, err)
	}
	return credentials, nil
}

// Write atomically creates or replaces an auth file with mode 0600.
// New parent directories are created with mode 0700.
func Write(path string, credentials Credentials) error {
	if err := credentials.validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create auth directory %q: %w", dir, err)
	}
	file, err := os.CreateTemp(dir, ".auth-*.tmp") // CreateTemp uses mode 0600.
	if err != nil {
		return fmt.Errorf("create auth file in %q: %w", dir, err)
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(credentials); err != nil {
		_ = file.Close()
		return fmt.Errorf("write auth file %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close auth file %q: %w", path, err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("save auth file %q: %w", path, err)
	}
	return nil
}
