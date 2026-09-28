package world

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/hazim-j/agent-wow/pkg/opcode"
	"github.com/hazim-j/agent-wow/pkg/worldconn"
)

// Wire formats follow AzerothCore revision
// d80ce1d87720e6b6a0b9adc952f21a658b1c245e: CharacterHandler.cpp,
// MovementHandler.cpp, MiscHandler.cpp, Player.cpp and WorldSession.cpp.
func (e *engine) handle(op uint16, body []byte) (bool, error) {
	if change, ok := controlChanges[op]; ok {
		return false, e.control(body, change)
	}
	switch op {
	case opcode.SMSGLoginVerifyWorld, opcode.SMSGNewWorld:
		if len(body) != 20 {
			return false, errors.New("invalid world location length")
		}
		r := decoder{data: body}
		mapID := r.u32()
		position := r.position()
		if err := r.finish(); err != nil {
			return false, err
		}
		space := "world"
		if op == opcode.SMSGNewWorld && e.transportTransfer {
			space = "transport"
		}
		e.location = location(mapID, position, space)
		e.movement = movement{pos: position}
		if op == opcode.SMSGLoginVerifyWorld {
			e.verified = true
		} else {
			e.phase, e.synced, e.awaitingWorld = Transferring, false, false
			if err := e.write(opcode.MSGMoveWorldportAck, nil); err != nil {
				return false, err
			}
		}
		e.publish()
		e.checkReady()
	case opcode.SMSGTransferPending:
		if len(body) != 4 && len(body) != 12 {
			return false, errors.New("invalid transfer notification")
		}
		e.transportTransfer = len(body) == 12
		e.phase, e.awaitingWorld = Transferring, true
		e.publish()
	case opcode.SMSGTransferAborted:
		if len(body) != 5 && len(body) != 6 {
			return false, errors.New("invalid transfer rejection")
		}
		return false, fmt.Errorf("map transfer rejected by server (reason %d)", body[4])
	case opcode.SMSGTimeSyncReq:
		if len(body) != 4 {
			return false, errors.New("invalid time-sync request")
		}
		response := binary.LittleEndian.AppendUint32(nil, binary.LittleEndian.Uint32(body))
		response = binary.LittleEndian.AppendUint32(response, e.timestamp())
		if err := e.write(opcode.CMSGTimeSyncResp, response); err != nil {
			return false, err
		}
		e.synced = true
		e.checkReady()
	case opcode.SMSGClientControlUpdate:
		r := decoder{data: body}
		guid, enabled := r.guid(), r.u8()
		if err := r.finish(); err != nil {
			return false, err
		}
		if enabled > 1 {
			return false, errors.New("invalid client-control flag")
		}
		if enabled == 1 {
			if guid != e.s.state.Character.GUID {
				return false, errors.New("control of other units is not supported")
			}
			return false, e.write(opcode.CMSGSetActiveMover, binary.LittleEndian.AppendUint64(nil, guid))
		}
	case opcode.MSGMoveTeleportAck:
		r := decoder{data: body}
		guid, counter := r.guid(), r.u32()
		m := r.movement()
		if err := r.finish(); err != nil {
			return false, err
		}
		if guid != e.s.state.Character.GUID {
			return false, nil
		}
		if e.location == nil {
			return false, errors.New("teleport received before map location")
		}
		response := appendGUID(nil, guid)
		response = binary.LittleEndian.AppendUint32(response, counter)
		response = binary.LittleEndian.AppendUint32(response, e.timestamp())
		if err := e.write(opcode.MSGMoveTeleportAck, response); err != nil {
			return false, err
		}
		e.movement = m
		e.location = location(e.location.MapID, m.pos, "world")
		e.publish()
	case opcode.SMSGTriggerCinematic, opcode.SMSGTriggerMovie:
		if len(body) != 4 {
			return false, errors.New("invalid cinematic/movie trigger")
		}
		response := uint32(opcode.CMSGCompleteCinematic)
		if op == opcode.SMSGTriggerMovie {
			response = opcode.CMSGCompleteMovie
		}
		return false, e.write(response, nil)
	case opcode.SMSGPong:
		if len(body) != 4 {
			return false, errors.New("invalid pong")
		}
		sequence := binary.LittleEndian.Uint32(body)
		if sent, ok := e.pings[sequence]; ok {
			if time.Since(sent) >= e.timing.pong {
				delete(e.pings, sequence)
				return false, nil
			}
			e.latency = uint32(time.Since(sent) / time.Millisecond)
			for id, at := range e.pings {
				if !at.After(sent) {
					delete(e.pings, id)
				}
			}
			e.pongTimer.Reset(e.timing.pong)
		}
	case opcode.SMSGCharacterLoginFailed:
		if len(body) != 1 {
			return false, errors.New("invalid login rejection")
		}
		return false, &LoginError{Code: body[0]}
	case opcode.SMSGLogoutResponse:
		if len(body) != 5 || body[4] > 1 {
			return false, errors.New("invalid logout response")
		}
		reason := binary.LittleEndian.Uint32(body)
		if reason != 0 && e.logout != nil {
			op := e.logout
			e.logout = nil
			e.logoutTimer.Stop()
			e.logoutDeadline = nil
			e.publish()
			op.err = &LogoutError{Reason: reason}
			close(op.done)
		}
	case opcode.SMSGLogoutComplete:
		if len(body) != 0 {
			return false, errors.New("invalid logout completion")
		}
		return true, nil
	case opcode.SMSGWardenData:
		reply, err := e.s.conn.WardenResponse(body)
		if err != nil {
			return false, err
		}
		if reply != nil {
			return false, e.write(opcode.CMSGWardenData, reply)
		}
	case opcode.SMSGMultipleMoves, opcode.SMSGCompressedMoves:
		return false, e.multipleMoves(op, body)
	}
	return false, nil // Ignore unrelated payloads without accumulating history.
}

func location(mapID uint32, pos [4]float32, space string) *Location {
	return &Location{MapID: mapID, X: pos[0], Y: pos[1], Z: pos[2], Orientation: pos[3], CoordinateSpace: space, ObservedAt: time.Now().UTC()}
}

type controlChange struct {
	ack              uint32
	flag             uint32
	enabled, applied bool
}

// The root and flight flags occur even for ordinary stationary logins/logout.
// The other pairs cover the stock aura initialization bundle.
var controlChanges = map[uint16]controlChange{
	opcode.SMSGForceMoveRoot:   {opcode.CMSGForceMoveRootAck, 0x00000800, true, false}, // root
	opcode.SMSGForceMoveUnroot: {opcode.CMSGForceMoveUnrootAck, 0x00000800, false, false},
	opcode.SMSGMoveSetCanFly:   {opcode.CMSGMoveSetCanFlyAck, 0x01000000, true, true}, // can fly
	opcode.SMSGMoveUnsetCanFly: {opcode.CMSGMoveSetCanFlyAck, 0x01000000, false, true},
	opcode.SMSGMoveWaterWalk:   {opcode.CMSGMoveWaterWalkAck, 0x10000000, true, true}, // water walk
	opcode.SMSGMoveLandWalk:    {opcode.CMSGMoveWaterWalkAck, 0x10000000, false, true},
	opcode.SMSGMoveFeatherFall: {opcode.CMSGMoveFeatherFallAck, 0x20000000, true, true}, // feather fall
	opcode.SMSGMoveNormalFall:  {opcode.CMSGMoveFeatherFallAck, 0x20000000, false, true},
	opcode.SMSGMoveSetHover:    {opcode.CMSGMoveHoverAck, 0x40000000, true, true}, // hover
	opcode.SMSGMoveUnsetHover:  {opcode.CMSGMoveHoverAck, 0x40000000, false, true},
}

func (e *engine) control(body []byte, change controlChange) error {
	r := decoder{data: body}
	guid, counter := r.guid(), r.u32()
	if err := r.finish(); err != nil {
		return err
	}
	if guid != e.s.state.Character.GUID {
		return nil
	}
	if e.location == nil {
		return errors.New("movement control received before map location")
	}
	if e.location.CoordinateSpace != "world" {
		return errors.New("movement control on a transport requires world-object tracking")
	}
	if change.enabled {
		e.movement.flags |= change.flag
	} else {
		e.movement.flags &^= change.flag
	}
	if change.flag == 0x800 && change.enabled {
		e.movement.flags &^= 0x04c030ff
	}
	if change.flag == 0x01000000 && !change.enabled {
		e.movement.flags &^= 0x02000000
	}
	body = appendGUID(nil, guid)
	body = binary.LittleEndian.AppendUint32(body, counter)
	body = e.movement.appendTo(body, e.timestamp())
	if change.applied {
		var applied uint32
		if change.enabled {
			applied = 1
		}
		body = binary.LittleEndian.AppendUint32(body, applied)
	}
	return e.write(change.ack, body)
}

func (e *engine) multipleMoves(op uint16, body []byte) error {
	if len(body) < 4 {
		return errors.New("truncated movement bundle")
	}
	size := int(binary.LittleEndian.Uint32(body))
	if size > worldconn.MaxPacketSize {
		return errors.New("movement bundle exceeds size limit")
	}
	data := body[4:]
	if op == opcode.SMSGCompressedMoves {
		input := bytes.NewReader(data)
		z, err := zlib.NewReader(input)
		if err != nil {
			return fmt.Errorf("movement bundle: %w", err)
		}
		data, err = io.ReadAll(io.LimitReader(z, int64(size)+1))
		closeErr := z.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if input.Len() != 0 {
			return errors.New("trailing compressed movement data")
		}
	}
	if len(data) != size {
		return errors.New("invalid movement bundle size")
	}
	for len(data) > 0 {
		n := int(data[0])
		data = data[1:]
		if n < 2 || n > len(data) {
			return errors.New("invalid embedded movement size")
		}
		embedded := binary.LittleEndian.Uint16(data)
		// Only movement-control subpackets are dispatched here. In particular,
		// never recurse into attacker-controlled nested compressed bundles.
		if change, ok := controlChanges[embedded]; ok {
			if err := e.control(data[2:n], change); err != nil {
				return err
			}
		}
		data = data[n:]
	}
	return nil
}
