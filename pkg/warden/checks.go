package warden

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/hazim-j/agent-wow/pkg/opcode"
)

type decoder struct {
	data []byte
	err  error
}

func (d *decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if n > len(d.data) {
		d.err = errors.New("truncated Warden check")
		return nil
	}
	b := d.data[:n]
	d.data = d.data[n:]
	return b
}

func (d *decoder) byte() byte {
	b := d.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func checks(body []byte, ticks uint32) ([]byte, error) {
	d := decoder{data: body}
	// The one-based string table ends with a zero length.
	table := []string{""}
	for {
		n := int(d.byte())
		if d.err != nil {
			return nil, d.err
		}
		if n == 0 {
			break
		}
		table = append(table, string(d.take(n)))
		if len(table) > 256 {
			return nil, errors.New("too many Warden strings")
		}
	}
	lookup := func(index byte) (string, error) {
		if index == 0 || int(index) >= len(table) {
			return "", errors.New("invalid Warden string index")
		}
		return table[index], nil
	}
	// Check IDs are XORed with the first byte of the current client key.
	xor := clientKey[0]
	if d.byte()^xor != opcode.WardenCheckTiming || d.err != nil {
		return nil, errors.New("missing Warden timing check")
	}
	result := binary.LittleEndian.AppendUint32([]byte{1}, ticks)
	for len(d.data) > 1 && d.err == nil {
		kind := d.byte() ^ xor
		switch kind {
		case opcode.WardenCheckMemory: // MEM_CHECK, relative to the emulated executable
			library := d.byte()
			request := d.take(5)
			if d.err != nil {
				return nil, d.err
			}
			address, length := binary.LittleEndian.Uint32(request), int(request[4])
			expected, ok := memoryProfile[address]
			if library != 0 || !ok || length != len(expected) {
				return nil, fmt.Errorf("unsupported Warden memory check: module %d, address 0x%x, length %d", library, address, length)
			}
			result = append(result, 0)
			result = append(result, expected...)
		case opcode.WardenCheckPageA, opcode.WardenCheckPageB: // PAGE_CHECK_A/B: no injected Windows pages in this profile
			d.take(28) // seed, hash, address
			if d.byte() == 0 && d.err == nil {
				return nil, errors.New("invalid Warden page length")
			}
			result = append(result, 0xe9)
		case opcode.WardenCheckModule: // MODULE_CHECK: no loaded Windows DLLs
			d.take(24) // seed, HMAC
			result = append(result, 0xe9)
		case opcode.WardenCheckDriver: // DRIVER_CHECK: no Windows drivers
			d.take(24)
			if _, err := lookup(d.byte()); err != nil {
				return nil, err
			}
			result = append(result, 0xe9)
		case opcode.WardenCheckLuaEval: // LUA_EVAL_CHECK
			source, err := lookup(d.byte())
			if err != nil {
				return nil, err
			}
			if !supportedLua(source) {
				return nil, errors.New("unsupported Warden Lua check")
			}
			result = append(result, 0, 0) // successful evaluation, empty returned string
		default:
			return nil, fmt.Errorf("unsupported Warden check type 0x%02x", kind)
		}
		if len(result) > maxPayload-7 {
			return nil, errors.New("Warden response too large")
		}
	}
	if d.err != nil {
		return nil, d.err
	}
	if len(d.data) != 1 || d.data[0] != xor {
		return nil, errors.New("invalid Warden check terminator")
	}
	return result, nil
}

func supportedLua(source string) bool {
	const prefix = "local S,T,R=SendAddonMessage,function()"
	const middle = " end R=S and T()if R then S('_TW',"
	const suffix = ",'GUILD')end"
	source, ok := strings.CutPrefix(source, prefix)
	if !ok {
		return false
	}
	source, ok = strings.CutSuffix(source, suffix)
	if !ok || len(source) < len(middle)+4 {
		return false
	}
	id := source[len(source)-4:]
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	predicate, ok := strings.CutSuffix(source[:len(source)-4], middle)
	return ok && luaProfile[predicate]
}
