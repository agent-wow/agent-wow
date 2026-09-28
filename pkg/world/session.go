package world

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hazim-j/agent-wow/pkg/worldconn"
)

// Session has one reader and one owner loop. Only the loop writes packets and
// changes protocol state; RPC callers read snapshots or submit commands.
type Session struct {
	conn      *worldconn.Conn
	logger    *slog.Logger
	mu        sync.RWMutex
	state     State
	err       error
	done      chan struct{}
	abort     chan struct{}
	closeOnce sync.Once
	logout    chan chan *logoutOperation
}

type logoutOperation struct {
	done chan struct{}
	err  error
}
type packet struct {
	opcode uint16
	body   []byte
	err    error
}

// Enter consumes an authenticated transport from worldconn.Dial, including on
// failure. The caller must stop using conn once it is passed to Enter. Applications
// normally use char.Client.EnterWorld, which verifies character ownership first.
// The context controls entry only. Close or Logout ends a successful session.
// Successful packet reads and writes use logger at debug level, including opcode
// and payload size but never payload data. A nil logger disables logging.
func Enter(ctx context.Context, conn *worldconn.Conn, realm Realm, character Character, logger *slog.Logger) (*Session, error) {
	return enter(ctx, conn, realm, character, logger, defaultTimings)
}

func enter(ctx context.Context, conn *worldconn.Conn, realm Realm, character Character, logger *slog.Logger, timing timings) (*Session, error) {
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if character.GUID == 0 {
		_ = conn.Close()
		return nil, errors.New("character GUID must not be zero")
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	s := &Session{conn: conn, logger: logger, state: State{Status: EnteringWorld, Realm: realm, Character: character}, done: make(chan struct{}), abort: make(chan struct{}), logout: make(chan chan *logoutOperation)}
	ready := make(chan struct{})
	go s.run(ready, timing)
	select {
	case <-ctx.Done():
		_ = s.Close()
		return nil, ctx.Err()
	case <-s.done:
		return nil, s.entryError()
	case <-ready:
		if err := ctx.Err(); err != nil {
			_ = s.Close()
			return nil, err
		}
		select {
		case <-s.done:
			return nil, s.entryError()
		default:
		}
		return s, nil
	}
}

func (s *Session) entryError() error {
	err := s.Err()
	if err == nil {
		err = errors.New("server logged the character out during entry")
	}
	return fmt.Errorf("enter world: %w", err)
}

func (s *Session) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := s.state
	if state.Location != nil {
		location := *state.Location
		state.Location = &location
	}
	return state
}

func (s *Session) Done() <-chan struct{} { return s.done }

// Err is meaningful after Done closes. A successful server logout returns nil.
func (s *Session) Err() error { s.mu.RLock(); defer s.mu.RUnlock(); return s.err }

// Close interrupts I/O and joins both session goroutines. It does not claim the
// server logged the character out; use Logout when graceful logout is required.
func (s *Session) Close() error {
	s.closeOnce.Do(func() { close(s.abort); _ = s.conn.Close() })
	<-s.done
	return nil
}

// Logout shares an outstanding logout operation. ctx cancels only this caller's
// wait; once submitted, the operation has its own 30-second lifetime.
func (s *Session) Logout(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	reply := make(chan *logoutOperation, 1)
	select {
	case <-s.done:
		return s.Err()
	case <-ctx.Done():
		return ctx.Err()
	case s.logout <- reply:
	}
	var op *logoutOperation
	select {
	case op = <-reply:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.Err()
	}
	select {
	case <-op.done:
		return op.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Session) run(ready chan struct{}, timing timings) {
	packets := make(chan packet, 1) // only one bounded payload queued ahead of the owner
	readerStop, readerDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			op, body, err := s.conn.ReadPacket()
			if err == nil {
				logPacket(s.logger, "read", uint32(op), len(body))
			}
			select {
			case packets <- packet{op, body, err}:
			case <-readerStop:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	e := &engine{s: s, timing: timing, started: time.Now(), ready: ready, phase: EnteringWorld, pings: make(map[uint32]time.Time)}
	err := e.loop(packets)
	close(readerStop)
	_ = s.conn.Close()
	<-readerDone
	if e.logoutTimer != nil {
		e.logoutTimer.Stop()
	}
	if e.pongTimer != nil {
		e.pongTimer.Stop()
	}
	s.mu.Lock()
	s.err = err
	s.state.Status = Closed
	if err != nil {
		s.state.Error = err.Error()
		if !errors.Is(err, ErrClosed) {
			s.state.Status = Failed
		}
	}
	s.mu.Unlock()
	if e.logout != nil {
		e.logout.err = err
		close(e.logout.done)
	}
	close(s.done)
}
