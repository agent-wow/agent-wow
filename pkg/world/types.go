// Package world implements a persistent, headless WoW 3.3.5a gameplay session.
// The server remains authoritative; State contains only the observations this
// client currently understands, not a complete replica of the game world.
package world

import (
	"errors"
	"fmt"
	"time"
)

type Status string

const (
	EnteringWorld Status = "entering_world"
	InWorld       Status = "in_world"
	Transferring  Status = "transferring"
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

// Location is the last server-reported position, not a simulated live position.
// Transport-relative transfers cannot be resolved without an object cache.
type Location struct {
	MapID           uint32    `json:"map_id"`
	X               float32   `json:"x"`
	Y               float32   `json:"y"`
	Z               float32   `json:"z"`
	Orientation     float32   `json:"orientation"`
	CoordinateSpace string    `json:"coordinate_space"`
	ObservedAt      time.Time `json:"observed_at"`
}

type State struct {
	Status    Status    `json:"status"`
	Realm     Realm     `json:"realm"`
	Character Character `json:"character"`
	Location  *Location `json:"location"`
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
