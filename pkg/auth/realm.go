package auth

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/agent-wow/agent-wow/pkg/opcode"
)

const (
	RealmFlagInvalid      byte = 0x01
	RealmFlagOffline      byte = 0x02
	RealmFlagSpecifyBuild byte = 0x04
)

// Realm is an authserver's realm-list entry for the authenticated account.
type Realm struct {
	ID         uint8
	Name       string
	Address    string
	Type       uint8
	Locked     bool
	Flags      uint8
	Population float32
	Characters uint8
	Timezone   uint8
	Version    [3]byte
	Build      uint16 // Present only when RealmFlagSpecifyBuild is set.
}

// Status describes whether this account can select the realm with build 12340.
func (r Realm) Status() string {
	var reasons []string
	if r.Flags&RealmFlagInvalid != 0 {
		reasons = append(reasons, "invalid")
	}
	if r.Flags&RealmFlagSpecifyBuild != 0 && r.Build != 12340 {
		reasons = append(reasons, "incompatible build")
	}
	if r.Flags&RealmFlagOffline != 0 {
		reasons = append(reasons, "offline")
	}
	if r.Locked {
		reasons = append(reasons, "locked")
	}
	if len(reasons) == 0 {
		return "online"
	}
	return strings.Join(reasons, ", ")
}

// Selectable reports whether the realm is online, compatible and unlocked.
func (r Realm) Selectable() bool { return r.Status() == "online" }

// ListRealms reconnects using a saved session and fetches the current realm list
// without a password or a new session key. Callers should set a context deadline.
func (c *Client) ListRealms(ctx context.Context, session *Session) ([]Realm, error) {
	var realms []Realm
	err := c.withSession(ctx, session, func(conn net.Conn) error {
		// REALM_LIST opcode followed by four reserved bytes.
		if _, err := io.Copy(conn, bytes.NewReader([]byte{opcode.AuthRealmList, 0, 0, 0, 0})); err != nil {
			return fmt.Errorf("send realm list request: %w", err)
		}
		var err error
		realms, err = readRealms(conn)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("fetch realm list: %w", err)
	}
	return realms, nil
}

// WoW 3.3.5a framing, as emitted by AzerothCore's AuthSession::RealmListCallback:
// https://github.com/azerothcore/azerothcore-wotlk/blob/master/src/server/apps/authserver/Server/AuthSession.cpp
func readRealms(r io.Reader) ([]Realm, error) {
	var header [3]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("read realm list header: %w", err)
	}
	if header[0] != opcode.AuthRealmList {
		return nil, fmt.Errorf("unexpected realm list opcode 0x%02x", header[0])
	}
	// The uint16 length bounds the allocation and all NUL-terminated strings.
	body := make([]byte, binary.LittleEndian.Uint16(header[1:]))
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read realm list body: %w", err)
	}
	if len(body) < 8 {
		return nil, errors.New("realm list body is too short")
	}
	count := int(binary.LittleEndian.Uint16(body[4:6]))
	data := bytes.NewBuffer(body[6 : len(body)-2])
	if count > data.Len()/12 { // Minimum record: 3 flags, two NULs, 7 trailing bytes.
		return nil, errors.New("invalid realm count")
	}
	realms := make([]Realm, 0, count)
	seen := make(map[uint8]bool, count)
	for i := 0; i < count; i++ {
		realm, err := readRealm(data)
		if err != nil {
			return nil, fmt.Errorf("read realm %d: %w", i+1, err)
		}
		if seen[realm.ID] {
			return nil, fmt.Errorf("duplicate realm ID %d", realm.ID)
		}
		seen[realm.ID] = true
		realms = append(realms, realm)
	}
	if data.Len() != 0 || !bytes.Equal(body[len(body)-2:], []byte{0x10, 0}) {
		return nil, errors.New("invalid realm list trailer")
	}
	return realms, nil
}

func readRealm(r *bytes.Buffer) (Realm, error) {
	var realm Realm
	var flags [3]byte
	if _, err := io.ReadFull(r, flags[:]); err != nil {
		return realm, err
	}
	realm.Type, realm.Locked, realm.Flags = flags[0], flags[1] != 0, flags[2]
	var err error
	if realm.Name, err = realmString(r); err != nil {
		return realm, fmt.Errorf("name: %w", err)
	}
	if realm.Address, err = realmString(r); err != nil {
		return realm, fmt.Errorf("address: %w", err)
	}
	host, port, err := net.SplitHostPort(realm.Address)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || host == "" || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return realm, errors.New("invalid realm address: expected host:port")
	}
	var tail [7]byte
	if _, err := io.ReadFull(r, tail[:]); err != nil {
		return realm, err
	}
	realm.Population = math.Float32frombits(binary.LittleEndian.Uint32(tail[:4]))
	if math.IsNaN(float64(realm.Population)) || math.IsInf(float64(realm.Population), 0) {
		return realm, errors.New("invalid realm population")
	}
	realm.Characters, realm.Timezone, realm.ID = tail[4], tail[5], tail[6]
	if realm.Flags&RealmFlagSpecifyBuild != 0 {
		var build [5]byte
		if _, err := io.ReadFull(r, build[:]); err != nil {
			return realm, fmt.Errorf("build: %w", err)
		}
		copy(realm.Version[:], build[:3])
		realm.Build = binary.LittleEndian.Uint16(build[3:])
	}
	return realm, nil
}

func realmString(r *bytes.Buffer) (string, error) {
	value, err := r.ReadString(0)
	if err != nil {
		return "", errors.New("unterminated string")
	}
	value = strings.TrimSuffix(value, "\x00")
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return "", errors.New("expected nonempty UTF-8 without control characters")
	}
	return value, nil
}
