package warden

import "encoding/hex"

// Wire constants and the emulated, unmodified build-12340 memory profile come
// from AzerothCore revision d80ce1d87720e6b6a0b9adc952f21a658b1c245e:
// src/server/game/Warden/Modules/WardenModuleWin.h,
// src/server/game/Warden/WardenWin.cpp, and data/sql/base/db_world/warden_checks.sql.
// These are compatibility data, not observations of the host process.
var (
	moduleID     = fromHex("79C0768D657977D697E10BAD956CCED1")
	moduleKey    = fromHex("AE25BC51063B77BD363C3EFE0FC173F9")
	moduleSeed   = fromHex("4D808D2C77D905C41A6380EC08586AFE")
	moduleHash   = fromHex("568C054C781A972A6037A2290C22B52571A06F4E")
	clientKey    = fromHex("7F96EEFDA5B63D20A4DF8E00CBF48304")
	serverKey    = fromHex("C2B7ADEDFCCCA9C2BFB3F85602BA809B")
	initializers = [][]byte{
		fromHex("01000100804F0200C01802003025020010290200"),
		fromHex("0400001092410001"),
		fromHex("01010020AE460001"),
	}
)

// Each entry is an exact address/length match; unknown ranges are unsupported.
var memoryProfile = map[uint32][]byte{
	4623652:  fromHex("578B7D08578BF1"),
	10010636: fromHex("8166443FFF1FFFD9565CD95E"),
	5417948:  fromHex("7734FF2485"),
	8491566:  fromHex("8B4D10890D"),
	5345746:  fromHex("746583F9177760"),
	7246064:  fromHex("8950108B450C"),
	7860712:  fromHex("742DF6407C"),
	10714892: fromHex("BB8D243FD4D0313E"),
	9990741:  fromHex("8B878000000089463C"),
	10000022: fromHex("894644894E54"),
	7517484:  fromHex("7518683B010000"),
	5081862:  fromHex("6840AAB600C60200"),
	7452688:  fromHex("8B81CC07000025000000"),
	5283280:  fromHex("558BECB8084E0000E8731DF0"),
	5265823:  fromHex("72118B5518"),
	11154396: fromHex("D893FEC0488C11C1"),
	5284488:  fromHex("7507C7451400000000"),
	5296496:  fromHex("558BEC81ECE80D00006A0AE8"),
	7739760:  fromHex("01BE80000000E805B6FFFF"),
	5124558:  fromHex("8BF08D4608"),
	5090917:  fromHex("E886EE1D0083C40C"),
	10694516: fromHex("2F549A416F12033B"),
	4609669:  fromHex("8986100F00"),
	5296823:  fromHex("75166824020000"),
	4198410:  fromHex("CCCCCCCCCCCC"),
	4609675:  fromHex("5E5DC20800"),
	11287980: fromHex("04000000903C9F00"),
	4618113:  fromHex("FF1554F79D003B470C89"),
	5345728:  fromHex("558B"),
	7726137:  fromHex("7414"),
	8016620:  fromHex("7417"),
	8016079:  fromHex("0F8462010000"),
	8054762:  fromHex("7506"),
	9995315:  fromHex("75440FB75E"),
}

// The emulated UI has no addons/chat messages or Lua unlocker. Only these stock
// predicates are supported; no received source code is evaluated.
var luaProfile = map[string]bool{
	"forceinsecure() return issecure()": true,
	"return not not PQR_IsMoving":       true,
	"local f=DEFAULT_CHAT_FRAME for i=1,f:GetNumMessages() do if (f:GetMessageInfo(i)):find(\"|cffffd200PQR|r\") then return true end end":       true,
	"local f=DEFAULT_CHAT_FRAME for i=1,f:GetNumMessages() do if (f:GetMessageInfo(i)):find(\"|cFF32CD32EWT|r\") then return true end end":       true,
	"local f=DEFAULT_CHAT_FRAME for i=1,f:GetNumMessages() do if (f:GetMessageInfo(i)):find(\"|cFFFF4400WoWPlus|r\") then return true end end":   true,
	"local f=DEFAULT_CHAT_FRAME for i=1,f:GetNumMessages() do if (f:GetMessageInfo(i)):find(\"Rotation Mode Disable\") then return true end end": true,
	"local f=DEFAULT_CHAT_FRAME for i=1,f:GetNumMessages() do if (f:GetMessageInfo(i)):find(\"Rotation Mode Enable\") then return true end end":  true,
}

func fromHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	} // Only compiled-in constants call this helper.
	return b
}
