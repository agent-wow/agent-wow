// Package modsession defines the identity and callbacks supplied by a gameplay session.
package modsession

import "context"

// Session supplies character identity, packet transport, and the shared monotonic
// clock. Both callbacks are required and must support concurrent calls.
type Session struct {
	Identity   Identity
	SendPacket func(context.Context, uint32, []byte) error
	Clock      func() uint32
}
