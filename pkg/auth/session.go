package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"unicode/utf8"

	"github.com/agent-wow/agent-wow/pkg/opcode"
)

// Session holds the verified credentials for a subsequent worldserver login.
// Key is secret and must not be logged or included in command output.
type Session struct {
	Username     string
	Key          [40]byte
	AccountFlags uint32
}

// CheckSession checks a saved key using AzerothCore's reconnect challenge/proof
// exchange. It does not need a password or replace the server's session key.
// The authserver is the address supplied to NewClient.
// Callers should set a context deadline. The connection is closed on return.
func (c *Client) CheckSession(ctx context.Context, session *Session) error {
	return c.withSession(ctx, session, nil)
}

// withSession keeps the reauthenticated connection open for a subsequent request.
func (c *Client) withSession(ctx context.Context, session *Session, request func(net.Conn) error) error {
	if session == nil || len(session.Username) == 0 || len(session.Username) > 17 ||
		!utf8.ValidString(session.Username) || strings.ContainsAny(session.Username, "\x00\r\n") ||
		session.Key == [40]byte{} {
		return errors.New("invalid session")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", c.address)
	if err != nil {
		return fmt.Errorf("connect to authserver %s: %w", c.address, err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("set authserver deadline: %w", err)
		}
	}
	err = reconnect(conn, session)
	if err == nil && request != nil {
		err = request(conn)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("authserver request: %w", ctx.Err())
	}
	return err
}

func reconnect(conn net.Conn, session *Session) error {
	username := upperLatin(session.Username)
	packet := logonChallenge(username)
	packet[0] = opcode.AuthReconnectChallenge
	if address, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		copy(packet[29:33], address.IP.To4())
	}
	if _, err := io.Copy(conn, bytes.NewReader(packet)); err != nil {
		return fmt.Errorf("send reconnect challenge: %w", err)
	}
	if err := readReconnectResult(conn, opcode.AuthReconnectChallenge); err != nil {
		return fmt.Errorf("reconnect challenge: %w", err)
	}
	var challenge [32]byte // Server nonce followed by the version challenge.
	if _, err := io.ReadFull(conn, challenge[:]); err != nil {
		return fmt.Errorf("read reconnect challenge: %w", err)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("create reconnect nonce: %w", err)
	}
	proof := digest([]byte(username), nonce[:], challenge[:16], session.Key[:])
	// AzerothCore's reconnect version proof hashes R1 with 20 zero bytes.
	var zeros [20]byte
	version := digest(nonce[:], zeros[:])
	packet = make([]byte, 58)
	packet[0] = opcode.AuthReconnectProof
	copy(packet[1:17], nonce[:])
	copy(packet[17:37], proof[:])
	copy(packet[37:57], version[:])
	if _, err := io.Copy(conn, bytes.NewReader(packet)); err != nil {
		return fmt.Errorf("send reconnect proof: %w", err)
	}
	if err := readReconnectResult(conn, opcode.AuthReconnectProof); err != nil {
		// AzerothCore closes the socket without a result when the key is stale.
		return fmt.Errorf("reconnect proof was not accepted (the saved session may be invalid): %w", err)
	}
	var flags [2]byte
	if _, err := io.ReadFull(conn, flags[:]); err != nil {
		return fmt.Errorf("read reconnect login flags: %w", err)
	}
	return nil
}

func readReconnectResult(r io.Reader, opcode byte) error {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	if header[0] != opcode {
		return fmt.Errorf("unexpected reconnect opcode 0x%02x", header[0])
	}
	if header[1] != 0 {
		return authError(header[1])
	}
	return nil
}
