package char

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/agent-wow/agent-wow/pkg/auth"
	"github.com/agent-wow/agent-wow/pkg/worldconn"
)

// Client owns an authenticated character-selection connection. Operations must
// be sequential. Close may be called concurrently to interrupt an operation.
// EnterWorld transfers the connection to a gameplay session permanently.
type Client struct {
	wire                *worldconn.Conn
	realm               auth.Realm
	expansion           uint8
	accountFlags        uint32
	ownership           sync.Mutex
	transferred, closed bool
	choose              func(int) int
}

func Dial(ctx context.Context, realm auth.Realm, session *auth.Session) (*Client, error) {
	wire, err := worldconn.Dial(ctx, realm, session)
	if err != nil {
		var rejected *worldconn.AuthError
		if errors.As(err, &rejected) {
			return nil, fmt.Errorf("authenticate to realm %q: %w", realm.Name, &ServerError{Operation: "auth", Code: rejected.Code})
		}
		return nil, err
	}
	return &Client{wire: wire, realm: realm, expansion: wire.Expansion(), accountFlags: session.AccountFlags}, nil
}

func (c *Client) checkOwner() error {
	c.ownership.Lock()
	defer c.ownership.Unlock()
	if c.transferred {
		return errors.New("character connection transferred to gameplay session")
	}
	if c.closed {
		return net.ErrClosed
	}
	return nil
}

func (c *Client) Close() error {
	c.ownership.Lock()
	defer c.ownership.Unlock()
	if c.transferred {
		return nil
	}
	c.closed = true
	return c.wire.Close()
}

func (c *Client) withContext(ctx context.Context, fn func() error) error {
	if err := c.checkOwner(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	if err := c.wire.SetDeadline(deadline); err != nil {
		return err
	}
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = c.Close(); close(finished) })
	err := fn()
	if !stop() {
		<-finished
	}
	if err != nil {
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		} else if nerr := (net.Error)(nil); errors.As(err, &nerr) && nerr.Timeout() && !deadline.IsZero() && !time.Now().Before(deadline) {
			err = errors.Join(err, context.DeadlineExceeded)
		}
		var serverErr *ServerError
		var unknown *OutcomeUnknownError
		if errors.As(err, &unknown) || !errors.As(err, &serverErr) {
			_ = c.Close()
		}
	} else if resetErr := c.wire.SetDeadline(time.Time{}); resetErr != nil {
		// A confirmed result stays confirmed even if cancellation closed the
		// connection immediately afterward. A future operation will fail closed.
		_ = c.Close()
	}
	return err
}
