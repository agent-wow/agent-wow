package opcode

// Warden payload commands from AzerothCore's src/server/game/Warden/Warden.h
// at revision d80ce1d87720e6b6a0b9adc952f21a658b1c245e.
// Client and server command values overlap; direction determines their meaning.
const (
	WardenCMSGModuleOK          = 0x01
	WardenCMSGCheatChecksResult = 0x02
	WardenCMSGHashResult        = 0x04

	WardenSMSGModuleUse          = 0x00
	WardenSMSGCheatChecksRequest = 0x02
	WardenSMSGModuleInitialize   = 0x03
	WardenSMSGHashRequest        = 0x05
)

// Warden check identifiers are XORed with the current client key's first byte
// inside a WardenSMSGCheatChecksRequest payload.
const (
	WardenCheckTiming  = 0x57
	WardenCheckDriver  = 0x71
	WardenCheckLuaEval = 0x8b
	WardenCheckPageA   = 0xb2
	WardenCheckPageB   = 0xbf
	WardenCheckModule  = 0xd9
	WardenCheckMemory  = 0xf3
)
