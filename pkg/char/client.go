package char

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

	"github.com/hazim-j/agent-wow/pkg/auth"
)

// Client owns an authenticated character-selection connection. Operations must
// be sequential. Close may be called concurrently to interrupt an operation.
type Client struct {
	conn         net.Conn
	realm        auth.Realm
	expansion    uint8
	accountFlags uint32
	send, recv   *rc4.Cipher
	closeOnce    sync.Once
	closeErr     error
	// Tests inject deterministic choices; production uses math/rand/v2.IntN.
	choose func(int) int
}

// Dial authenticates to the supplied realm using a saved session. The context
// governs setup only; each subsequent operation has its own context.
func Dial(ctx context.Context, realm auth.Realm, session *auth.Session) (*Client, error) {
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
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", realm.Address)
	if err != nil {
		return nil, fmt.Errorf("connect to realm %q: %w", realm.Name, err)
	}
	c := &Client{conn: conn, realm: realm, accountFlags: session.AccountFlags}
	err = c.withContext(ctx, func() error { return c.authenticate(session) })
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("authenticate to realm %q: %w", realm.Name, err)
	}
	return c, nil
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.conn.Close() })
	return c.closeErr
}

func (c *Client) withContext(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	if err := c.conn.SetDeadline(deadline); err != nil {
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
	} else if resetErr := c.conn.SetDeadline(time.Time{}); resetErr != nil {
		// A confirmed result stays confirmed even if cancellation closed the
		// connection immediately afterward. A future operation will fail closed.
		_ = c.Close()
	}
	return err
}

// WorldSocket.cpp::HandleAuthSession, revision d80ce1d87720e6b6a0b9adc952f21a658b1c245e.
func (c *Client) authenticate(session *auth.Session) error {
	opcode, challenge, err := c.readPacket()
	if err != nil {
		return err
	}
	// IP bans can be rejected before the server issues its challenge.
	if opcode == smsgAuthResponse && len(challenge) == 1 {
		return &ServerError{Operation: "auth", Code: challenge[0]}
	}
	if opcode != smsgAuthChallenge || len(challenge) != 40 {
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
	if _, err := c.writePacket(cmsgAuthSession, body); err != nil {
		return err
	}
	c.send, c.recv = headerCipher(session.Key, true), headerCipher(session.Key, false)
	for range 4096 {
		body, err := c.waitPacket(smsgAuthResponse)
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
			return &ServerError{Operation: "auth", Code: body[0]}
		}
	}
	return errors.New("too many authentication queue updates")
}
