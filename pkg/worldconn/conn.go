// Package worldconn owns the authenticated build-12340 worldserver transport.
// One reader and one writer may operate concurrently. Writes (including cipher
// advancement) are serialized; protocol dispatch belongs to the caller.
package worldconn

import (
	"context"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/agent-wow/agent-wow/pkg/auth"
	"github.com/agent-wow/agent-wow/pkg/opcode"
	"github.com/agent-wow/agent-wow/pkg/warden"
)

// AuthError preserves the server's world authentication rejection code.
type AuthError struct{ Code uint8 }

func (e *AuthError) Error() string {
	return fmt.Sprintf("world authentication rejected (0x%02x)", e.Code)
}

// Conn owns one worldserver connection and its header and Warden cipher state.
// Use Dial to obtain an authenticated connection. The zero value is not usable.
type Conn struct {
	conn       net.Conn
	realm      auth.Realm
	expansion  uint8
	send, recv *rc4.Cipher
	writeMu    sync.Mutex
	closeOnce  sync.Once
	closeErr   error
	warden     *warden.Client
}

// New wraps a raw connection before header encryption has started. It does not
// authenticate or initialize Warden; use Dial for an authenticated worldserver.
func New(conn net.Conn) *Conn                      { return &Conn{conn: conn} }
func (c *Conn) Expansion() uint8                   { return c.expansion }
func (c *Conn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
func (c *Conn) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.conn.Close() })
	return c.closeErr
}

// Dial connects to a realm and authenticates using a saved session.
// Its context governs startup only, never the authenticated connection lifetime.
func Dial(ctx context.Context, realm auth.Realm, session *auth.Session) (*Conn, error) {
	if session == nil || session.Key == [40]byte{} || len(session.Username) == 0 || len(session.Username) > 17 || !utf8.ValidString(session.Username) || strings.ContainsFunc(session.Username, unicode.IsControl) {
		return nil, errors.New("invalid saved session; run 'agent-wow auth login'")
	}
	if !realm.Selectable() {
		return nil, fmt.Errorf("realm %q is %s", realm.Name, realm.Status())
	}
	host, port, err := net.SplitHostPort(realm.Address)
	portNum, portErr := strconv.Atoi(port)
	if err != nil || host == "" || portErr != nil || portNum < 1 || portNum > 65535 {
		return nil, errors.New("realm address must be host:port with a valid TCP port")
	}
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", realm.Address)
	if err != nil {
		return nil, fmt.Errorf("connect to realm %q: %w", realm.Name, err)
	}
	c := New(raw)
	c.realm = realm
	deadline, _ := ctx.Deadline()
	if err = c.SetDeadline(deadline); err != nil {
		_ = c.Close()
		return nil, err
	}
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = c.Close(); close(finished) })
	err = c.authenticate(session)
	if !stop() {
		<-finished
	}
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	if err == nil {
		err = c.SetDeadline(time.Time{})
	}
	if err != nil {
		_ = c.Close()
		var nerr net.Error
		if errors.As(err, &nerr) && nerr.Timeout() && !deadline.IsZero() && !time.Now().Before(deadline) {
			err = errors.Join(err, context.DeadlineExceeded)
		}
		return nil, fmt.Errorf("authenticate to realm %q: %w", realm.Name, err)
	}
	return c, nil
}

// WorldSocket.cpp::HandleAuthSession, revision d80ce1d87720e6b6a0b9adc952f21a658b1c245e.
func (c *Conn) authenticate(session *auth.Session) error {
	op, challenge, err := c.ReadPacket()
	if err != nil {
		return err
	}
	// IP bans can be rejected before the server issues its challenge.
	if op == opcode.SMSGAuthResponse && len(challenge) == 1 {
		return &AuthError{Code: challenge[0]}
	}
	if op != opcode.SMSGAuthChallenge || len(challenge) != 40 {
		return errors.New("invalid realm authentication challenge")
	}
	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	username := session.Username // auth.Login already applies AzerothCore's normalization.
	h := sha1.New()
	h.Write([]byte(username))
	h.Write([]byte{0, 0, 0, 0})
	h.Write(nonce[:])
	h.Write(challenge[4:8])
	h.Write(session.Key[:])
	body := binary.LittleEndian.AppendUint32(nil, 12340)
	body = binary.LittleEndian.AppendUint32(body, 0) // LoginServerID
	body = append(body, []byte(username)...)
	body = append(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0) // LoginServerType
	body = append(body, nonce[:]...)
	body = binary.LittleEndian.AppendUint32(body, 0) // RegionID
	body = binary.LittleEndian.AppendUint32(body, 0) // BattlegroupID
	body = binary.LittleEndian.AppendUint32(body, uint32(c.realm.ID))
	body = binary.LittleEndian.AppendUint64(body, 0) // DosResponse
	body = append(body, h.Sum(nil)...)
	body = binary.LittleEndian.AppendUint32(body, 0) // Empty addon block
	if _, err := c.WritePacket(opcode.CMSGAuthSession, body); err != nil {
		return err
	}
	c.send, c.recv = HeaderCipher(session.Key, true), HeaderCipher(session.Key, false)
	c.warden = warden.New(session.Key)
	for range 4096 {
		body, err := c.waitPacket(opcode.SMSGAuthResponse)
		if err != nil {
			return fmt.Errorf("read authentication response (saved session may be stale): %w", err)
		}
		if len(body) == 0 {
			return errors.New("empty authentication response")
		}
		switch body[0] {
		case 0x0c: // AUTH_OK; queue release uses the one-byte form.
			if len(body) != 1 && len(body) != 11 {
				return errors.New("invalid authentication success response")
			}
			if len(body) == 11 {
				c.expansion = body[10]
			}
			return nil
		case 0x1b: // AUTH_WAIT_QUEUE: initial full form and compact updates.
			if len(body) != 6 && len(body) != 16 {
				return errors.New("invalid authentication queue response")
			}
			if len(body) == 16 {
				c.expansion = body[10]
			}
		default:
			return &AuthError{Code: body[0]}
		}
	}
	return errors.New("too many authentication queue updates")
}

func (c *Conn) waitPacket(expected uint16) ([]byte, error) {
	// Warden starts during authentication and continues on the same connection.
	// Other unrelated startup packets are drained.
	for range 4096 {
		op, body, err := c.ReadPacket()
		if err != nil {
			return nil, err
		}
		if op == expected {
			return body, nil
		}
		if op == opcode.SMSGWardenData {
			reply, err := c.WardenResponse(body)
			if err != nil {
				return nil, err
			}
			if reply != nil {
				if _, err := c.WritePacket(opcode.CMSGWardenData, reply); err != nil {
					return nil, err
				}
			}
		}
		if op == opcode.SMSGAuthResponse {
			if len(body) == 0 {
				return nil, errors.New("empty authentication response")
			}
			if body[0] != 0x0c {
				return nil, &AuthError{Code: body[0]}
			}
		}
	}
	return nil, errors.New("too many unrelated realm packets")
}

// WardenResponse advances the connection's Warden protocol state. The current
// protocol owner must call it sequentially and send any returned payload as
// CMSG_WARDEN_DATA using its normal write deadline. ReadPacket never writes.
func (c *Conn) WardenResponse(body []byte) ([]byte, error) {
	if c.warden == nil {
		return nil, errors.New("Warden received before authentication")
	}
	return c.warden.Handle(body)
}
