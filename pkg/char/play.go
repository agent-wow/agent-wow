package char

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/hazim-j/agent-wow/pkg/world"
)

// EnterWorld verifies ownership and permanently hands this client's existing
// connection to a gameplay session. The context governs entry, not gameplay.
// Close on this Client is harmless after handoff; selection cannot be resumed.
// The session uses logger for packet activity; nil disables logging.
func (c *Client) EnterWorld(ctx context.Context, guid GUID, logger *slog.Logger) (*world.Session, error) {
	if guid == 0 {
		return nil, errors.New("character GUID must not be zero")
	}
	var selected Character
	err := c.withContext(ctx, func() error {
		characters, err := c.list()
		if err != nil {
			return err
		}
		for _, ch := range characters {
			if ch.GUID == guid {
				selected = ch
				return nil
			}
		}
		return fmt.Errorf("character GUID %d is not in this account's current character list", guid)
	})
	if err != nil {
		return nil, err
	}
	c.ownership.Lock()
	if c.closed {
		c.ownership.Unlock()
		return nil, net.ErrClosed
	}
	c.transferred = true
	c.ownership.Unlock()
	return world.Enter(ctx, c.wire, world.Realm{ID: c.realm.ID, Name: c.realm.Name}, world.Character{GUID: uint64(selected.GUID), Name: selected.Name}, logger)
}
