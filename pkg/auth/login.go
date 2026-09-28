package auth

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"unicode/utf8"

	"github.com/hazim-j/agent-wow/pkg/opcode"
)

// Login performs a challenge/proof exchange and verifies the server's
// proof before returning a session. The connection is closed on return or context
// cancellation. Callers should set a context deadline to bound the whole login.
func (c *Client) Login(ctx context.Context, username, password string) (*Session, error) {
	if len(username) == 0 || len(username) > 17 || !utf8.ValidString(username) || strings.ContainsAny(username, "\x00\r\n") {
		return nil, errors.New("username must contain 1 to 17 UTF-8 bytes without NUL or newlines")
	}
	if password == "" || !utf8.ValidString(password) {
		return nil, errors.New("password must be nonempty UTF-8")
	}
	username, password = upperLatin(username), upperLatin(password)
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", c.address)
	if err != nil {
		return nil, fmt.Errorf("connect to authserver %s: %w", c.address, err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set authserver deadline: %w", err)
		}
	}
	session, err := exchange(conn, username, password)
	if ctx.Err() != nil {
		return nil, fmt.Errorf("authenticate: %w", ctx.Err())
	}
	return session, err
}

func exchange(conn net.Conn, username, password string) (*Session, error) {
	packet := logonChallenge(username)
	if address, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		copy(packet[29:33], address.IP.To4())
	}
	if _, err := io.Copy(conn, bytes.NewReader(packet)); err != nil {
		return nil, fmt.Errorf("send logon challenge: %w", err)
	}
	c, err := readChallenge(conn)
	if err != nil {
		return nil, fmt.Errorf("logon challenge: %w", err)
	}
	p, err := newProof(username, password, c)
	if err != nil {
		return nil, fmt.Errorf("create logon proof: %w", err)
	}
	packet = make([]byte, 75)
	packet[0] = opcode.AuthLogonProof
	copy(packet[1:33], p.public[:])
	copy(packet[33:53], p.client[:])
	// CRC/version proof, key count and security flags are zero. Custom client
	// integrity checks and PIN/matrix/authenticator challenges are unsupported.
	if _, err := io.Copy(conn, bytes.NewReader(packet)); err != nil {
		return nil, fmt.Errorf("send logon proof: %w", err)
	}
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, fmt.Errorf("read logon proof result: %w", err)
	}
	if header[0] != opcode.AuthLogonProof {
		return nil, fmt.Errorf("unexpected logon proof opcode 0x%02x", header[0])
	}
	if header[1] != 0 {
		return nil, fmt.Errorf("logon proof: %w", authError(header[1]))
	}
	var response [30]byte // M2, account flags, survey ID, login flags.
	if _, err := io.ReadFull(conn, response[:]); err != nil {
		return nil, fmt.Errorf("read logon proof: %w", err)
	}
	if subtle.ConstantTimeCompare(response[:20], p.server[:]) != 1 {
		return nil, errors.New("authserver proof verification failed")
	}
	return &Session{Username: username, Key: p.key, AccountFlags: binary.LittleEndian.Uint32(response[20:24])}, nil
}

func logonChallenge(username string) []byte {
	packet := make([]byte, 34+len(username))
	packet[0] = opcode.AuthLogonChallenge
	packet[1] = 8
	binary.LittleEndian.PutUint16(packet[2:4], uint16(len(packet)-4))
	copy(packet[4:8], "WoW\x00")
	copy(packet[8:11], []byte{3, 3, 5})
	binary.LittleEndian.PutUint16(packet[11:13], 12340)
	copy(packet[13:17], "68x\x00")
	copy(packet[17:21], "niW\x00")
	copy(packet[21:25], "SUne")
	packet[33] = byte(len(username))
	copy(packet[34:], username)
	return packet
}

func readChallenge(r io.Reader) (challenge, error) {
	var c challenge
	var header [3]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return c, err
	}
	if header[0] != opcode.AuthLogonChallenge {
		return c, fmt.Errorf("unexpected opcode 0x%02x", header[0])
	}
	if header[2] != 0 {
		return c, authError(header[2])
	}
	if _, err := io.ReadFull(r, c.public[:]); err != nil {
		return c, err
	}
	// Only accept AzerothCore's fixed group, never arbitrary server parameters.
	var generator [2]byte
	if _, err := io.ReadFull(r, generator[:1]); err != nil {
		return c, err
	}
	if generator[0] != 1 {
		return c, errors.New("unsupported SRP6 generator length")
	}
	if _, err := io.ReadFull(r, generator[1:]); err != nil {
		return c, err
	}
	var group [33]byte
	if _, err := io.ReadFull(r, group[:1]); err != nil {
		return c, err
	}
	if group[0] != 32 {
		return c, errors.New("unsupported SRP6 modulus length")
	}
	if _, err := io.ReadFull(r, group[1:]); err != nil {
		return c, err
	}
	n := toLittleEndian(modulus)
	if generator[1] != 7 || !bytes.Equal(group[1:], n[:]) {
		return c, errors.New("unsupported SRP6 group")
	}
	if _, err := io.ReadFull(r, c.salt[:]); err != nil {
		return c, err
	}
	var tail [17]byte // Version challenge and security flags.
	if _, err := io.ReadFull(r, tail[:]); err != nil {
		return c, err
	}
	if tail[16] != 0 {
		return c, fmt.Errorf("unsupported account security challenge (flags 0x%02x): PIN, matrix and authenticator login are not implemented", tail[16])
	}
	return c, nil
}

func authError(code byte) error {
	message := map[byte]string{
		0x03: "account or IP is banned",
		0x04: "incorrect account name or password",
		0x05: "incorrect password",
		0x06: "account is already online",
		0x07: "no game time remaining",
		0x08: "authserver database is busy",
		0x09: "client version rejected (requires WoW 3.3.5a build 12340 without custom integrity checks)",
		0x0a: "client update required",
		0x0b: "invalid server",
		0x0c: "account is suspended",
		0x0d: "account access denied",
		0x0f: "parental controls prevent login",
		0x10: "account is locked to another IP address",
		0x18: "account is locked",
		0x19: "account is locked to another country",
	}[code]
	if message == "" {
		message = "authentication rejected"
	}
	return fmt.Errorf("%s (code 0x%02x)", message, code)
}

// Match AzerothCore's Utf8ToUpperOnlyLatin: only ASCII a-z are uppercased.
func upperLatin(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}, value)
}
