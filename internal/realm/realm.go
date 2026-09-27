// Package realm manages the client's local realm.json file.
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

// Realm is the on-disk realm.json format.
type Realm struct {
	ID      uint8  `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

// Read loads and validates a realm file. A missing file can be detected with
// errors.Is(err, os.ErrNotExist).
func Read(path string) (Realm, error) {
	var selection Realm
	file, err := os.Open(path)
	if err != nil {
		return selection, fmt.Errorf("read realm file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return selection, fmt.Errorf("read realm file %q: %w", path, err)
	}
	if len(data) > 65536 {
		return selection, fmt.Errorf("realm file %q is too large", path)
	}
	if err := json.Unmarshal(data, &selection); err != nil {
		return Realm{}, fmt.Errorf("invalid realm file %q: expected a JSON object with id, name and address fields", path)
	}
	if err := selection.validate(); err != nil {
		return Realm{}, fmt.Errorf("invalid realm file %q: %w", path, err)
	}
	return selection, nil
}

// Write atomically creates or replaces a realm file with mode 0600.
// New parent directories are created with mode 0700.
func Write(path string, selection Realm) error {
	if err := selection.validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create realm directory %q: %w", dir, err)
	}
	file, err := os.CreateTemp(dir, ".realm-*.tmp") // CreateTemp uses mode 0600.
	if err != nil {
		return fmt.Errorf("create realm file in %q: %w", dir, err)
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(selection); err != nil {
		_ = file.Close()
		return fmt.Errorf("write realm file %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close realm file %q: %w", path, err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("save realm file %q: %w", path, err)
	}
	return nil
}

func (r Realm) validate() error {
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
