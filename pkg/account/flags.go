// Package account describes AzerothCore account attributes.
package account

import (
	"fmt"
	"strings"
)

// FormatFlags returns the hexadecimal account bitmask and the meaning of each
// enabled flag. Flags with undocumented meanings are explicitly labeled.
func FormatFlags(flags uint32) string {
	if flags == 0 {
		return "0x00000000 (none)"
	}
	// AzerothCore account.Flags definitions:
	// https://www.azerothcore.org/wiki/account#flags
	// Keep undocumented meanings explicit instead of inferring permissions.
	definitions := [...]struct {
		bit         uint32
		description string
	}{
		{0x00000001, "GM: game master account"},
		{0x00000002, "NOKICK: exempt from AFK logout"},
		{0x00000004, "COLLECTOR: Collector's Edition starter gift"},
		{0x00000008, "TRIAL: trial account"},
		{0x00000010, "CANCELLED: meaning unknown"},
		{0x00000020, "IGR: Internet Game Room"},
		{0x00000040, "WHOLESALER: meaning unknown"},
		{0x00000080, "PRIVILEGED: meaning unknown"},
		{0x00000100, "EU_FORBID_ELV: meaning unknown"},
		{0x00000200, "EU_FORBID_BILLING: meaning unknown"},
		{0x00000400, "RESTRICTED: meaning unknown"},
		{0x00000800, "REFERRAL: Recruit-A-Friend participant"},
		{0x00001000, "BLIZZARD: meaning unknown"},
		{0x00002000, "RECURRING_BILLING: meaning unknown"},
		{0x00004000, "NOELECTUP: meaning unknown"},
		{0x00008000, "KR_CERTIFICATE: possibly a Korean certificate"},
		{0x00010000, "EXPANSION_COLLECTOR: Burning Crusade Collector's Edition"},
		{0x00020000, "DISABLE_VOICE: voice chat unavailable"},
		{0x00040000, "DISABLE_VOICE_SPEAK: voice chat speaking unavailable"},
		{0x00080000, "REFERRAL_RESURRECT: Scroll of Resurrection"},
		{0x00100000, "EU_FORBID_CC: meaning unknown"},
		{0x00200000, "OPENBETA_DELL: Dell XPS WoW promotion"},
		{0x00400000, "PROPASS: meaning unknown"},
		{0x00800000, "PROPASS_LOCK: Pro Pass (Arena Tournament)"},
		{0x01000000, "PENDING_UPGRADE: meaning unknown"},
		{0x02000000, "RETAIL_FROM_TRIAL: meaning unknown"},
		{0x04000000, "EXPANSION2_COLLECTOR: Wrath of the Lich King Collector's Edition"},
		{0x08000000, "OVERMIND_LINKED: Battle.net account linked"},
		{0x10000000, "DEMOS: meaning unknown"},
		{0x20000000, "DEATH_KNIGHT_OK: Death Knight creation level requirement met"},
		{0x40000000, "S2_REQUIRE_IGR: meaning unknown"},
		{0x80000000, "S2_TRIAL: meaning unknown"},
	}
	var meanings []string
	for _, flag := range definitions {
		if flags&flag.bit != 0 {
			meanings = append(meanings, flag.description)
		}
	}
	return fmt.Sprintf("0x%08X (%s)", flags, strings.Join(meanings, "; "))
}
