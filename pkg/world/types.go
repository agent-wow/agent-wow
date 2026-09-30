// Package world implements a persistent, headless WoW 3.3.5a gameplay session.
// State contains connection lifecycle and identity only; modules own gameplay.
package world

import (
	"errors"
	"fmt"
)

type Status string

const (
	EnteringWorld Status = "entering_world"
	InWorld       Status = "in_world"
	LoggingOut    Status = "logging_out"
	Closed        Status = "closed"
	Failed        Status = "failed"
)

type Realm struct {
	ID   uint8  `json:"id"`
	Name string `json:"name"`
}

type Character struct {
	GUID uint64 `json:"guid,string"`
	Name string `json:"name"`
}

type State struct {
	Status    Status    `json:"status"`
	Realm     Realm     `json:"realm"`
	Character Character `json:"character"`
	Error     string    `json:"error,omitempty"`
}

var ErrClosed = errors.New("gameplay session closed without confirmed logout")

type LoginError struct{ Code uint8 }

func (e *LoginError) Error() string {
	return fmt.Sprintf("enter world rejected by server (0x%02x)", e.Code)
}

type LogoutError struct{ Reason uint32 }

func (e *LogoutError) Error() string {
	return fmt.Sprintf("logout rejected by server (reason %d)", e.Reason)
}
