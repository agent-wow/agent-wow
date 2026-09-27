package char

import (
	"crypto/hmac"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	cmsgCharCreate    = 0x036
	cmsgCharEnum      = 0x037
	cmsgCharDelete    = 0x038
	smsgCharCreate    = 0x03a
	smsgCharEnum      = 0x03b
	smsgCharDelete    = 0x03c
	smsgAuthChallenge = 0x1ec
	cmsgAuthSession   = 0x1ed
	smsgAuthResponse  = 0x1ee
	maxPacketSize     = 1 << 20
)

// Header encryption is required by this legacy protocol; payloads are plaintext.
// AuthCrypt.cpp (build 12340), viewed from the CLIENT's direction:
// https://github.com/azerothcore/azerothcore-wotlk/blob/d80ce1d87720e6b6a0b9adc952f21a658b1c245e/src/common/Cryptography/Authentication/AuthCrypt.cpp
func headerCipher(key [40]byte, sending bool) *rc4.Cipher {
	seed := []byte{0xcc, 0x98, 0xae, 0x04, 0xe8, 0x97, 0xea, 0xca, 0x12, 0xdd, 0xc0, 0x93, 0x42, 0x91, 0x53, 0x57}
	if sending {
		seed = []byte{0xc2, 0xb3, 0x72, 0x3c, 0xc6, 0xae, 0xd9, 0xb5, 0x34, 0x3c, 0x53, 0xee, 0x2f, 0x43, 0x67, 0xce}
	}
	h := hmac.New(sha1.New, seed)
	h.Write(key[:])
	cipher, _ := rc4.NewCipher(h.Sum(nil)) // SHA-1 always supplies a valid 20-byte key.
	var discard [1024]byte
	cipher.XORKeyStream(discard[:], discard[:])
	return cipher
}

func (c *Client) readPacket() (uint16, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(c.conn, header[:1]); err != nil {
		return 0, nil, err
	}
	if c.recv != nil {
		c.recv.XORKeyStream(header[:1], header[:1])
	}
	headerLen := 4
	if header[0]&0x80 != 0 {
		headerLen = 5
	}
	if _, err := io.ReadFull(c.conn, header[1:headerLen]); err != nil {
		return 0, nil, err
	}
	if c.recv != nil {
		c.recv.XORKeyStream(header[1:headerLen], header[1:headerLen])
	}
	size := int(binary.BigEndian.Uint16(header[:2]))
	if headerLen == 5 {
		size = int(header[0]&0x7f)<<16 | int(header[1])<<8 | int(header[2])
	}
	if size < 2 || size > maxPacketSize {
		return 0, nil, fmt.Errorf("invalid realm packet size %d", size)
	}
	opcode := binary.LittleEndian.Uint16(header[headerLen-2 : headerLen])
	body := make([]byte, size-2)
	if _, err := io.ReadFull(c.conn, body); err != nil {
		return 0, nil, err
	}
	return opcode, body, nil
}

// The bool reports whether any part of the request was written. This matters
// when classifying a failed mutation whose result could be unknown.
func (c *Client) writePacket(opcode uint32, body []byte) (bool, error) {
	if len(body)+4 >= 10240 {
		return false, errors.New("outgoing realm packet is too large")
	}
	header := binary.BigEndian.AppendUint16(nil, uint16(len(body)+4))
	header = binary.LittleEndian.AppendUint32(header, opcode)
	if c.send != nil {
		c.send.XORKeyStream(header, header)
	}
	packet := append(header, body...)
	written := false
	for len(packet) > 0 {
		n, err := c.conn.Write(packet)
		written = written || n > 0
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
		packet = packet[n:]
	}
	return written, nil
}

func (c *Client) waitPacket(expected uint16) ([]byte, error) {
	// Addon, cache, tutorial, account-data and Warden module packets can arrive
	// around authentication. Character selection does not consume their payloads.
	for range 4096 {
		opcode, body, err := c.readPacket()
		if err != nil {
			return nil, err
		}
		if opcode == expected {
			return body, nil
		}
		if opcode == smsgAuthResponse {
			if len(body) == 0 {
				return nil, errors.New("empty authentication response")
			}
			if body[0] != 0x0c {
				return nil, &ServerError{Operation: "auth", Code: body[0]}
			}
		}
	}
	return nil, errors.New("too many unrelated realm packets")
}
