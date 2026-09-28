package char

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hazim-j/agent-wow/pkg/opcode"
)

func (c *Client) List(ctx context.Context) ([]Character, error) {
	var characters []Character
	err := c.withContext(ctx, func() error {
		var err error
		characters, err = c.list()
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list characters: %w", err)
	}
	return characters, nil
}

func (c *Client) list() ([]Character, error) {
	if _, err := c.writePacket(opcode.CMSGCharEnum, nil); err != nil {
		return nil, err
	}
	body, err := c.waitPacket(opcode.SMSGCharEnum)
	if err != nil {
		return nil, err
	}
	return decodeCharacters(body)
}

// Player::BuildEnumData emits every field, including the 23 nine-byte
// equipment/bag display slots. Consume the complete record, not just CLI fields.
func decodeCharacters(body []byte) ([]Character, error) {
	if len(body) < 1 {
		return nil, errors.New("empty character list response")
	}
	count := int(body[0])
	if count > (len(body)-1)/270 {
		return nil, errors.New("invalid character count")
	}
	r := bytes.NewBuffer(body[1:])
	characters := make([]Character, 0, count)
	seen := make(map[GUID]bool, count)
	for i := 0; i < count; i++ {
		var guid [8]byte
		if _, err := io.ReadFull(r, guid[:]); err != nil {
			return nil, err
		}
		name, err := r.ReadString(0)
		if err != nil {
			return nil, errors.New("unterminated character name")
		}
		name = strings.TrimSuffix(name, "\x00")
		if name == "" || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) || utf8.RuneCountInString(name) > 12 {
			return nil, errors.New("invalid character name in response")
		}
		var tail [261]byte
		if _, err := io.ReadFull(r, tail[:]); err != nil {
			return nil, fmt.Errorf("truncated character record: %w", err)
		}
		ch := Character{GUID: GUID(binary.LittleEndian.Uint64(guid[:])), Name: name,
			Race: Race(tail[0]), Class: Class(tail[1]), Gender: Gender(tail[2]),
			Appearance: Appearance{Skin: tail[3], Face: tail[4], HairStyle: tail[5], HairColor: tail[6], FacialHair: tail[7]},
			Level:      tail[8], ZoneID: binary.LittleEndian.Uint32(tail[9:13]), MapID: binary.LittleEndian.Uint32(tail[13:17])}
		if ch.GUID == 0 || seen[ch.GUID] {
			return nil, errors.New("invalid or duplicate character GUID")
		}
		for _, offset := range []int{17, 21, 25} {
			v := math.Float32frombits(binary.LittleEndian.Uint32(tail[offset : offset+4]))
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return nil, errors.New("invalid character position")
			}
		}
		seen[ch.GUID] = true
		characters = append(characters, ch)
	}
	if r.Len() != 0 {
		return nil, errors.New("trailing data in character list")
	}
	return characters, nil
}

// Delete verifies that the GUID belongs to this account on this realm before
// sending the deletion request. It does not prompt or retry the mutation.
func (c *Client) Delete(ctx context.Context, guid GUID) error {
	if guid == 0 {
		return errors.New("character GUID must not be zero")
	}
	return c.withContext(ctx, func() error {
		characters, err := c.list()
		if err != nil {
			return err
		}
		for _, ch := range characters {
			if ch.GUID != guid {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return c.mutate("delete", opcode.CMSGCharDelete, opcode.SMSGCharDelete, 0x47, binary.LittleEndian.AppendUint64(nil, uint64(guid)), "", guid)
		}
		return fmt.Errorf("character GUID %d is not in this account's current character list", guid)
	})
}

func (c *Client) mutate(operation string, request uint32, response uint16, success byte, body []byte, name string, guid GUID) error {
	sent, err := c.writePacket(request, body)
	if err == nil {
		var result []byte
		result, err = c.waitPacket(response)
		if err == nil {
			if len(result) != 1 {
				err = errors.New("invalid mutation response length")
			} else if result[0] == success {
				return nil
			} else if result[0] == 0x2e || result[0] == 0x46 {
				err = errors.New("server did not return a final mutation result")
			} else {
				return &ServerError{Operation: operation, Code: result[0]}
			}
		}
	}
	if sent {
		return &OutcomeUnknownError{Operation: operation, Name: name, GUID: guid, Err: err}
	}
	return err
}
