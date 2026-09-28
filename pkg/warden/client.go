// Package warden implements the stock AzerothCore build-12340 Warden protocol.
// It emulates a fixed client profile; it never loads downloaded executable code,
// executes server-supplied Lua, or exposes the host process's memory/files.
package warden

import (
	"bytes"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/hazim-j/agent-wow/pkg/opcode"
)

const maxPayload = 4096

type phase uint8

const (
	module phase = iota
	hash
	initialize
	active
)

// Client belongs to the connection's protocol owner. Handle must be sequential.
// Its payload ciphers are separate from world packet header encryption.
type Client struct {
	send, recv  *rc4.Cipher
	phase       phase
	initialized int
	started     time.Time
}

// New creates a Warden client using the authenticated session's 40-byte key.
// Create one Client per worldserver connection and retain it for that
// connection's lifetime. The zero value of Client is not initialized.
func New(key [40]byte) *Client {
	// SessionKeyGenerator<SHA1>: concatenate SHA1(left, previous, right),
	// starting with a zero previous digest; use the first two 16-byte keys.
	left, right := sha1.Sum(key[:20]), sha1.Sum(key[20:])
	var previous [20]byte
	var material []byte
	for len(material) < 32 {
		h := sha1.New()
		h.Write(left[:])
		h.Write(previous[:])
		h.Write(right[:])
		copy(previous[:], h.Sum(nil))
		material = append(material, previous[:]...)
	}
	send, _ := rc4.NewCipher(material[:16])
	recv, _ := rc4.NewCipher(material[16:32])
	return &Client{send: send, recv: recv, started: time.Now()}
}

// Handle consumes one encrypted SMSG_WARDEN_DATA payload and returns an
// encrypted CMSG_WARDEN_DATA payload, or nil when no reply is needed. Any error
// is terminal: cipher state has advanced and the connection must be closed.
func (c *Client) Handle(payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > maxPayload {
		return nil, errors.New("invalid Warden payload size")
	}
	body := make([]byte, len(payload))
	c.recv.XORKeyStream(body, payload)
	var response []byte
	rekey := false
	switch body[0] {
	case opcode.WardenSMSGModuleUse: // MODULE_USE: the known module is implemented locally.
		if c.phase != module || len(body) != 37 {
			return nil, errors.New("invalid Warden module request")
		}
		if !bytes.Equal(body[1:17], moduleID) || !bytes.Equal(body[17:33], moduleKey) || binary.LittleEndian.Uint32(body[33:]) != 18756 {
			return nil, fmt.Errorf("unsupported Warden module %x (only stock AzerothCore build 12340 is supported)", body[1:17])
		}
		response = []byte{opcode.WardenCMSGModuleOK} // MODULE_OK; no module download is needed.
		c.phase = hash
	case opcode.WardenSMSGHashRequest: // HASH_REQUEST
		if c.phase != hash || len(body) != 17 {
			return nil, errors.New("invalid Warden hash request")
		}
		if !bytes.Equal(body[1:], moduleSeed) {
			return nil, errors.New("unsupported Warden module seed")
		}
		response = append([]byte{opcode.WardenCMSGHashResult}, moduleHash...)
		rekey = true
		c.phase = initialize
	case opcode.WardenSMSGModuleInitialize: // MODULE_INITIALIZE: records may share a packet.
		if c.phase != initialize {
			return nil, errors.New("unexpected Warden initialization")
		}
		for len(body) > 0 {
			if len(body) < 7 || body[0] != opcode.WardenSMSGModuleInitialize || c.initialized >= len(initializers) {
				return nil, errors.New("invalid Warden initialization record")
			}
			n := int(binary.LittleEndian.Uint16(body[1:]))
			if n > len(body)-7 || checksum(body[7:7+n]) != binary.LittleEndian.Uint32(body[3:]) {
				return nil, errors.New("invalid Warden initialization length or checksum")
			}
			if !bytes.Equal(body[7:7+n], initializers[c.initialized]) {
				return nil, errors.New("unsupported Warden initialization functions")
			}
			c.initialized++
			body = body[7+n:]
		}
		if c.initialized == len(initializers) {
			c.phase = active
		}
	case opcode.WardenSMSGCheatChecksRequest: // CHEAT_CHECKS_REQUEST
		if c.phase != active {
			return nil, errors.New("Warden checks before module initialization")
		}
		result, err := checks(body[1:], uint32(time.Since(c.started)/time.Millisecond))
		if err != nil {
			return nil, err
		}
		response = binary.LittleEndian.AppendUint16([]byte{opcode.WardenCMSGCheatChecksResult}, uint16(len(result)))
		response = binary.LittleEndian.AppendUint32(response, checksum(result))
		response = append(response, result...)
	default:
		return nil, fmt.Errorf("unsupported Warden command 0x%02x", body[0])
	}
	if response != nil {
		c.send.XORKeyStream(response, response)
	}
	// The hash reply uses the old key. Subsequent packets use the module keys.
	if rekey {
		c.send, _ = rc4.NewCipher(clientKey)
		c.recv, _ = rc4.NewCipher(serverKey)
	}
	return response, nil
}

func checksum(data []byte) uint32 {
	sum := sha1.Sum(data)
	var result uint32
	for i := 0; i < len(sum); i += 4 {
		result ^= binary.LittleEndian.Uint32(sum[i:])
	}
	return result
}
