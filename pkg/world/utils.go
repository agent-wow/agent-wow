package world

import (
	"fmt"
	"log/slog"

	"github.com/agent-wow/agent-wow/pkg/opcode"
)

func logPacket(logger *slog.Logger, direction string, op uint32, size int) {
	logger.Debug("world packet", "direction", direction, "opcode", fmt.Sprintf("0x%03x", op), "opcode_label", opcode.WorldName(op), "bytes", size)
}
