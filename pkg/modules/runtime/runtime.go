// Package modrt manages module lifecycle and routes calls and packets for a gameplay session.
package modrt

import (
	"context"
	"encoding/json"

	"github.com/hazim-j/agent-wow/pkg/modules/session"
)

// Runtime is the module lifecycle and invocation contract used by a world session.
// Done signals fatal failure; Stop cancels work; Close joins workers and cleans up.
// Blocking operations must run outside the world connection's owner loop.
type Runtime interface {
	Start(context.Context, modsession.Session) error
	Publish(uint16, []byte) error
	FlushPackets(context.Context) error
	InvokeJSON(context.Context, string, json.RawMessage) (json.RawMessage, error)
	BeginLogout()
	PrepareLogout(context.Context) error
	Resume()
	Done() <-chan struct{}
	Err() error
	Stop()
	Close() error
}

// CallError carries JSON-RPC error metadata and the underlying module invocation error.
type CallError struct {
	Code           int
	Module, Method string
	Err            error
}

func (e *CallError) Error() string { return e.Err.Error() }

func (e *CallError) Unwrap() error { return e.Err }
