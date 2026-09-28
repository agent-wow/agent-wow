package opcode

// WorldName returns the protocol label for a world packet opcode defined in
// this package, or "UNKNOWN" for an unrecognized value.
func WorldName(op uint32) string {
	if op < uint32(len(worldNames)) {
		return worldNames[op]
	}
	return "UNKNOWN"
}
