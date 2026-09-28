// Package clientdata shares the catalogue tools' local reference-data location
// and pinned upstream versions. Only the fetch command accesses the network.
package clientdata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	Directory     = "tmp/client-data"
	ReleaseURL    = "https://github.com/wowgaming/client-data/releases/tag/v20.0"
	ArchiveURL    = "https://github.com/wowgaming/client-data/releases/download/v20.0/Data.zip"
	CoreRevision  = "d80ce1d87720e6b6a0b9adc952f21a658b1c245e"
	ClassesURL    = "https://raw.githubusercontent.com/azerothcore/azerothcore-wotlk/" + CoreRevision + "/data/sql/base/db_world/playercreateinfo.sql"
	OpcodesURL    = "https://raw.githubusercontent.com/azerothcore/azerothcore-wotlk/" + CoreRevision + "/src/server/game/Server/Protocol/Opcodes.h"
	OpcodesSHA256 = "9109ea4d2abd098883acd1e6b552104adf8965544478439811c75426315aa447"
)

// Read reads a local reference file, explaining how to fetch missing data.
func Read(directory, name string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(directory, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("client data is missing; run './run client-data:fetch' first: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("read client data %s: %w", name, err)
	}
	return data, nil
}
