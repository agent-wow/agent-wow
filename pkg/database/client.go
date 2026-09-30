// Package database persists client sessions in a Badger database.
package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/agent-wow/agent-wow/pkg/auth"
	"github.com/dgraph-io/badger/v4"
)

// ErrSessionNotFound indicates that no session has been saved.
var ErrSessionNotFound = errors.New("no saved session")

// Client owns a database connection. Call Close when it is no longer needed.
type Client struct {
	db *badger.DB
}

// NewClient opens a database in dataDir, creating the directory with mode 0700
// if necessary. The directory contains secret session keys.
func NewClient(dataDir string) (*Client, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("database directory must not be empty")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := badger.Open(badger.DefaultOptions(dataDir).WithLogger(nil).WithSyncWrites(true))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &Client{db: db}, nil
}

// Close flushes the database and releases its directory lock.
func (c *Client) Close() error {
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

// A slice ensures decoding rejects session keys of the wrong length; JSON
// decoding into a fixed array would silently truncate or pad malformed keys.
type storedSession struct {
	Username     string `json:"username"`
	Key          []byte `json:"key"`
	AccountFlags uint32 `json:"account_flags"`
}

const sessionKey = "session"

// SaveSession atomically replaces the current session after a successful login.
// It stores no password, and errors never include the session key.
func (c *Client) SaveSession(session *auth.Session) error {
	if session == nil {
		return errors.New("cannot save a nil session")
	}
	stored := storedSession{session.Username, session.Key[:], session.AccountFlags}
	if err := stored.validate(); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return errors.New("encode session")
	}
	if err := c.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(sessionKey), data)
	}); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// GetSession returns the most recently saved session, or ErrSessionNotFound.
// Loading a session does not establish whether the authserver still accepts it.
func (c *Client) GetSession() (*auth.Session, error) {
	var stored storedSession
	err := c.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(sessionKey))
		if err != nil {
			return err
		}
		return item.Value(func(data []byte) error {
			if err := json.Unmarshal(data, &stored); err != nil {
				return errors.New("invalid saved session encoding")
			}
			return stored.validate()
		})
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	session := &auth.Session{Username: stored.Username, AccountFlags: stored.AccountFlags}
	copy(session.Key[:], stored.Key)
	return session, nil
}

func (s storedSession) validate() error {
	if len(s.Username) == 0 || len(s.Username) > 17 || !utf8.ValidString(s.Username) || strings.ContainsAny(s.Username, "\x00\r\n") {
		return errors.New("invalid saved session username")
	}
	if len(s.Key) != 40 || [40]byte(s.Key) == [40]byte{} {
		return errors.New("invalid saved session key")
	}
	return nil
}
