package char

import "fmt"

// ServerError preserves an explicit server rejection's protocol result code.
type ServerError struct {
	Operation string
	Code      uint8
}

func (e *ServerError) Error() string {
	message := resultMessages[e.Code]
	if message == "" {
		message = "unknown server result"
	}
	return fmt.Sprintf("character %s: %s (0x%02x)", e.Operation, message, e.Code)
}

// OutcomeUnknownError means the request may have reached the server, but its
// result could not be confirmed. Do not retry without checking the live list.
type OutcomeUnknownError struct {
	Operation string
	Name      string
	GUID      GUID
	Err       error
}

func (e *OutcomeUnknownError) Error() string {
	target := fmt.Sprintf("GUID %d", e.GUID)
	if e.Operation == "create" {
		target = fmt.Sprintf("name %q", e.Name)
	}
	return fmt.Sprintf("character %s outcome unknown for %s; run 'agent-wow char list' before retrying: %v", e.Operation, target, e.Err)
}

func (e *OutcomeUnknownError) Unwrap() error { return e.Err }

// SharedDefines.h, ResponseCodes, AzerothCore revision d80ce1d87720e6b6a0b9adc952f21a658b1c245e.
var resultMessages = map[uint8]string{
	0x0d: "authentication failed; run 'agent-wow auth login'",
	0x0e: "realm rejected authentication", 0x0f: "bad server proof",
	0x10: "realm unavailable", 0x11: "authentication system error",
	0x12: "billing error", 0x13: "billing expired", 0x14: "client version mismatch",
	0x15: "unknown account", 0x16: "incorrect password",
	0x17: "session expired; run 'agent-wow auth login'", 0x18: "server shutting down",
	0x19: "already logging in", 0x1a: "login server not found", 0x1b: "waiting in realm queue",
	0x1c: "account banned", 0x1d: "account already online", 0x1e: "no game time",
	0x1f: "server database busy", 0x20: "account suspended", 0x21: "parental controls",
	0x22: "account locked", 0x27: "realm not found", 0x2d: "character list failed",
	0x30: "character creation error", 0x31: "character creation failed",
	0x32: "name already in use", 0x33: "character creation disabled",
	0x34: "opposite factions are not permitted on this PvP realm",
	0x35: "realm character limit reached", 0x36: "account character limit reached",
	0x37: "creation server queue", 0x38: "creation restricted to existing accounts",
	0x39: "race requires a newer account expansion", 0x3a: "class requires a newer account expansion",
	0x3b: "Death Knight level requirement not met", 0x3c: "Death Knight limit reached",
	0x3d: "character is in a guild", 0x3e: "race/class combination restricted",
	0x3f: "choose a character race", 0x40: "character is an arena leader",
	0x41: "character has mail", 0x42: "faction change required", 0x43: "race restriction",
	0x44: "character gold limit", 0x45: "character login required",
	0x48: "character deletion failed", 0x49: "character locked for transfer",
	0x4a: "cannot delete a guild leader", 0x4b: "cannot delete an arena team captain",
	0x58: "invalid character name", 0x59: "name must not be empty", 0x5a: "name too short",
	0x5b: "name too long", 0x5c: "invalid name characters", 0x5d: "name mixes languages",
	0x5e: "name rejected for profanity", 0x5f: "reserved name", 0x60: "invalid apostrophe",
	0x61: "multiple apostrophes", 0x62: "three consecutive identical name characters",
	0x63: "invalid space in name", 0x64: "consecutive spaces in name",
	0x65: "consecutive Russian silent characters", 0x66: "invalid Russian silent character position",
	0x67: "name declension mismatch",
}
