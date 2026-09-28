package char

import (
	"errors"

	"github.com/hazim-j/agent-wow/pkg/opcode"
)

func (c *Client) readPacket() (uint16, []byte, error) { return c.wire.ReadPacket() }
func (c *Client) writePacket(op uint32, body []byte) (bool, error) {
	return c.wire.WritePacket(op, body)
}

func (c *Client) waitPacket(expected uint16) ([]byte, error) {
	// Maintain Warden while draining unrelated character-selection traffic.
	for range 4096 {
		op, body, err := c.readPacket()
		if err != nil {
			return nil, err
		}
		if op == expected {
			return body, nil
		}
		if op == opcode.SMSGWardenData {
			reply, err := c.wire.WardenResponse(body)
			if err != nil {
				return nil, err
			}
			if reply != nil {
				if _, err := c.writePacket(opcode.CMSGWardenData, reply); err != nil {
					return nil, err
				}
			}
		}
		if op == opcode.SMSGAuthResponse {
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
