package world

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/hazim-j/agent-wow/pkg/opcode"
)

type timings struct{ ping, pong, logout, write time.Duration }

var defaultTimings = timings{30 * time.Second, 90 * time.Second, 30 * time.Second, 10 * time.Second}

// engine is private to the owner loop; none of its fields require locks.
type engine struct {
	s                     *Session
	timing                timings
	started               time.Time
	ready                 chan struct{}
	phase                 Status
	verified, synced      bool
	location              *Location
	movement              movement
	transportTransfer     bool
	awaitingWorld         bool
	logout                *logoutOperation
	logoutTimer           *time.Timer
	logoutDeadline        <-chan time.Time
	pongTimer             *time.Timer
	pingSequence, latency uint32
	pings                 map[uint32]time.Time
}

func (e *engine) write(op uint32, body []byte) error {
	if err := e.s.conn.SetWriteDeadline(time.Now().Add(e.timing.write)); err != nil {
		return err
	}
	_, err := e.s.conn.WritePacket(op, body)
	if err == nil {
		logPacket(e.s.logger, "write", op, len(body))
	}
	return err
}

func (e *engine) timestamp() uint32 { return uint32(time.Since(e.started) / time.Millisecond) }

func (e *engine) publish() {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	e.s.state.Status = e.phase
	if e.logout != nil {
		e.s.state.Status = LoggingOut
	}
	if e.location != nil {
		location := *e.location
		e.s.state.Location = &location
	}
}

func (e *engine) checkReady() {
	if !e.verified || !e.synced || e.awaitingWorld {
		return
	}
	e.phase = InWorld
	e.publish()
	if e.ready != nil {
		close(e.ready)
		e.ready = nil
	}
}

func (e *engine) loop(packets <-chan packet) error {
	if err := e.write(opcode.CMSGPlayerLogin, binary.LittleEndian.AppendUint64(nil, e.s.state.Character.GUID)); err != nil {
		return err
	}
	ping := time.NewTimer(e.timing.ping)
	defer ping.Stop()
	e.pongTimer = time.NewTimer(e.timing.pong)
	for {
		select {
		case <-e.s.abort:
			return ErrClosed
		case reply := <-e.s.logout:
			if e.logout != nil {
				reply <- e.logout
				continue
			}
			op := &logoutOperation{done: make(chan struct{})}
			reply <- op
			if e.phase != InWorld {
				op.err = fmt.Errorf("cannot log out while session is %s", e.phase)
				close(op.done)
				continue
			}
			e.logout = op
			e.publish()
			e.logoutTimer = time.NewTimer(e.timing.logout)
			e.logoutDeadline = e.logoutTimer.C
			if err := e.write(opcode.CMSGLogoutRequest, nil); err != nil {
				return err
			}
		case <-e.logoutDeadline:
			return errors.New("logout timed out without server confirmation")
		case <-e.pongTimer.C:
			return errors.New("worldserver pong timeout")
		case <-ping.C:
			now := time.Now()
			// Expire unanswered requests even if delayed replies keep arriving.
			// This bounds memory and prevents an ancient pong renewing liveness.
			for id, sent := range e.pings {
				if now.Sub(sent) >= e.timing.pong {
					delete(e.pings, id)
				}
			}
			e.pingSequence++
			body := binary.LittleEndian.AppendUint32(nil, e.pingSequence)
			body = binary.LittleEndian.AppendUint32(body, e.latency)
			e.pings[e.pingSequence] = now
			if err := e.write(opcode.CMSGPing, body); err != nil {
				return err
			}
			// Space actual sends apart, including after a temporarily blocked
			// write; AzerothCore rejects clients which send pings too quickly.
			ping.Reset(e.timing.ping)
		case p := <-packets:
			if p.err != nil {
				select {
				case <-e.s.abort:
					return ErrClosed
				default:
				}
				return fmt.Errorf("worldserver disconnected: %w", p.err)
			}
			complete, err := e.handle(p.opcode, p.body)
			if err != nil {
				return fmt.Errorf("world packet 0x%03x: %w", p.opcode, err)
			}
			if complete {
				return nil
			}
		}
	}
}
