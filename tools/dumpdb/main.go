// dumpdb prints the dev database as JSON, opening Badger in read-only mode.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/dgraph-io/badger/v4"
	"github.com/hazim-j/agent-wow/internal/config"
)

func main() {
	if err := config.Init("dev.config.yaml"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := dump(config.Get().DataDir, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dump(dir string, output io.Writer) (err error) {
	db, err := badger.Open(badger.DefaultOptions(dir).WithReadOnly(true).WithLogger(nil))
	if err != nil {
		return fmt.Errorf("open dev database: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	values := make(map[string]any)
	if err := db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			item := iterator.Item()
			value, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			// Preserve structured JSON; other values are encoded as base64.
			if json.Valid(value) {
				values[string(item.Key())] = json.RawMessage(value)
			} else {
				values[string(item.Key())] = value
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("read dev database: %w", err)
	}

	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(values)
}
