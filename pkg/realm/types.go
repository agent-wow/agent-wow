// Package realm describes AzerothCore realm attributes.
package realm

import "strconv"

// FormatType returns the display name for a realm type from the authserver's
// realm list. Unknown types are returned as their decimal identifier.
func FormatType(value uint8) string {
	switch value {
	case 0:
		return "Normal"
	case 1:
		return "PvP"
	case 6:
		return "RP"
	case 8:
		return "RP-PvP"
	default:
		return strconv.Itoa(int(value))
	}
}
