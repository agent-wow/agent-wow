// Package realm manages the client's persisted realm selection.
package realm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Selection is persisted in config_dir/realm.json. The authserver is supplied
// by the main configuration.
type Selection struct {
	ID      uint8  `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

var current struct {
	configDir string
	selection *Selection
}

// Init loads the selected realm from configDir. Call Init after
// validating the main config, before using the realm selection.
func Init(configDir string) error {
	selection, err := load(filepath.Join(configDir, "realm.json"))
	if err != nil {
		return err
	}
	current.configDir = configDir
	current.selection = selection
	return nil
}

// Path returns the file used to save the selected realm.
func Path() string { return filepath.Join(current.configDir, "realm.json") }

// Get returns a copy of the selected realm, or nil when no realm is selected.
func Get() *Selection {
	if current.selection == nil {
		return nil
	}
	selection := *current.selection
	return &selection
}

func load(path string) (*Selection, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read selected realm config: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return nil, fmt.Errorf("read selected realm config: %w", err)
	}
	var realm Selection
	if len(data) > 65536 || json.Unmarshal(data, &realm) != nil {
		return nil, fmt.Errorf("invalid selected realm config %q", path)
	}
	if err := realm.validate(); err != nil {
		return nil, fmt.Errorf("invalid selected realm config %q: %w", path, err)
	}
	return &realm, nil
}

// Save atomically saves a selection and updates the loaded selection only
// after the write succeeds. It leaves the main config and its overrides intact.
func Save(realm Selection) error {
	if err := realm.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(current.configDir, 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	file, err := os.CreateTemp(current.configDir, ".realm-*.tmp")
	if err != nil {
		return fmt.Errorf("create selected realm config: %w", err)
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(realm); err != nil {
		_ = file.Close()
		return fmt.Errorf("write selected realm config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close selected realm config: %w", err)
	}
	if err := os.Rename(file.Name(), Path()); err != nil {
		return fmt.Errorf("save selected realm config: %w", err)
	}
	current.selection = &realm
	return nil
}

func (r Selection) validate() error {
	if strings.TrimSpace(r.Name) == "" || !utf8.ValidString(r.Name) || strings.ContainsFunc(r.Name, unicode.IsControl) {
		return errors.New("selected realm name must be nonempty UTF-8 without control characters")
	}
	host, port, err := net.SplitHostPort(r.Address)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || strings.TrimSpace(host) == "" || strings.ContainsFunc(host, unicode.IsSpace) ||
		strings.ContainsFunc(host, unicode.IsControl) || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("selected realm address must be host:port with a valid TCP port")
	}
	return nil
}
