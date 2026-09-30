// Command charcatalog generates the stock build-12340 compatibility catalogue
// from local reference files. It does not access the network or server internals.
//
// Run from the repository root:
//
//	./run client-data:fetch
//	./run charcatalog:generate
//
// The tmp/client-data directory must contain CharSections.dbc, CharHairGeosets.dbc,
// CharacterFacialHairStyles.dbc and playercreateinfo.sql. Only numeric creation
// choices and source checksums are retained in pkg/char/profiles_gen.go.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"github.com/agent-wow/agent-wow/tools/internal/clientdata"
)

const destination = "pkg/char/profiles_gen.go"

type sourceHashes struct {
	Sections string
	Hair     string
	Facial   string
	Classes  string
}

type metadata struct {
	Build        int
	Source       string
	CoreRevision string
	SHA256       sourceHashes
	RaceClasses  [][2]uint32
}

type profile struct {
	Race        uint32
	Gender      uint32
	DeathKnight bool
	SkinFaces   [][2]uint32
	Hair        [][3]uint32
}

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("Usage: ./run charcatalog:generate\nReads tmp/client-data and writes pkg/char/profiles_gen.go. Run './run client-data:fetch' first.")
		return
	}
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: ./run charcatalog:generate")
		os.Exit(2)
	}
	data, pairs, profiles, err := generate(clientdata.Directory)
	if err == nil {
		err = writeCatalogue(destination, data)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate character catalogue:", err)
		os.Exit(1)
	}
	fmt.Printf("Generated %d race/class pairs and %d appearance profiles (%d bytes)\n", pairs, profiles, len(data))
}

func generate(directory string) ([]byte, int, int, error) {
	info := metadata{Build: 12340, Source: clientdata.ReleaseURL, CoreRevision: clientdata.CoreRevision}
	read := func(name string, hash *string) ([]byte, error) {
		data, err := clientdata.Read(directory, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		*hash = fmt.Sprintf("%x", sha256.Sum256(data))
		return data, nil
	}
	readTable := func(name string, fields int, hash *string) ([][]uint32, error) {
		data, err := read(name, hash)
		if err != nil {
			return nil, err
		}
		rows, err := decodeDBC(data, fields)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return rows, nil
	}
	sections, err := readTable("CharSections.dbc", 10, &info.SHA256.Sections)
	if err != nil {
		return nil, 0, 0, err
	}
	hair, err := readTable("CharHairGeosets.dbc", 6, &info.SHA256.Hair)
	if err != nil {
		return nil, 0, 0, err
	}
	facial, err := readTable("CharacterFacialHairStyles.dbc", 8, &info.SHA256.Facial)
	if err != nil {
		return nil, 0, 0, err
	}
	sql, err := read("playercreateinfo.sql", &info.SHA256.Classes)
	if err != nil {
		return nil, 0, 0, err
	}
	info.RaceClasses, err = parseRaceClasses(sql)
	if err != nil {
		return nil, 0, 0, err
	}
	var profiles []profile
	var previousRace uint32
	for _, pair := range info.RaceClasses {
		if pair[0] == previousRace {
			continue
		}
		previousRace = pair[0]
		for gender := uint32(0); gender < 2; gender++ {
			for _, dk := range []bool{false, true} {
				p, err := buildProfile(pair[0], gender, dk, sections, hair, facial)
				if err != nil {
					return nil, 0, 0, err
				}
				profiles = append(profiles, p)
			}
		}
	}
	data, err := encodeCatalogue(info, profiles)
	return data, len(info.RaceClasses), len(profiles), err
}

func decodeDBC(data []byte, fields int) ([][]uint32, error) {
	if len(data) < 20 || string(data[:4]) != "WDBC" {
		return nil, errors.New("expected a complete WDBC header")
	}
	count := binary.LittleEndian.Uint32(data[4:8])
	columns := binary.LittleEndian.Uint32(data[8:12])
	size := binary.LittleEndian.Uint32(data[12:16])
	stringsSize := binary.LittleEndian.Uint32(data[16:20])
	if columns != uint32(fields) || size != uint32(fields)*4 {
		return nil, fmt.Errorf("expected %d fields and %d bytes per record, got %d and %d", fields, fields*4, columns, size)
	}
	// Check in uint64 before allocating or indexing using untrusted header counts.
	if 20+uint64(count)*uint64(size)+uint64(stringsSize) != uint64(len(data)) {
		return nil, errors.New("file length does not match DBC record and string counts")
	}
	rows := make([][]uint32, int(count))
	for i := range rows {
		rows[i] = make([]uint32, fields)
		for j := range rows[i] {
			offset := 20 + i*int(size) + j*4
			rows[i][j] = binary.LittleEndian.Uint32(data[offset : offset+4])
		}
	}
	return rows, nil
}

var raceClassPattern = regexp.MustCompile(`(?m)^\((\d+),(\d+),`)

func parseRaceClasses(sql []byte) ([][2]uint32, error) {
	unique := make(map[[2]uint32]bool)
	for _, match := range raceClassPattern.FindAllSubmatch(sql, -1) {
		var pair [2]uint32
		for i := range pair {
			n, err := strconv.ParseUint(string(match[i+1]), 10, 8)
			if err != nil || n == 0 {
				return nil, fmt.Errorf("invalid race/class ID %q in playercreateinfo.sql", match[i+1])
			}
			pair[i] = uint32(n)
		}
		unique[pair] = true
	}
	if len(unique) < 60 {
		return nil, fmt.Errorf("expected at least 60 race/class pairs in playercreateinfo.sql, got %d", len(unique))
	}
	return sortedPairs(unique), nil
}

func sortedPairs(values map[[2]uint32]bool) [][2]uint32 {
	result := make([][2]uint32, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.SortFunc(result, func(a, b [2]uint32) int { return slices.Compare(a[:], b[:]) })
	return result
}

func buildProfile(race, gender uint32, dk bool, sections, hairGeo, facialGeo [][]uint32) (profile, error) {
	p := profile{Race: race, Gender: gender, DeathKnight: dk}
	var rows [][]uint32
	skins := make(map[uint32]bool)
	facialTextures := make(map[[2]uint32]bool)
	for _, row := range sections {
		// Bit 1 marks playable sections; bit 4 restricts a section to Death
		// Knights. Other flags do not replace these requirements.
		if row[1] != race || row[2] != gender || row[7]&1 == 0 || !dk && row[7]&4 != 0 {
			continue
		}
		rows = append(rows, row)
		if row[3] == 0 {
			skins[row[9]] = true
		}
		if row[3] == 2 {
			facialTextures[[2]uint32{row[8], row[9]}] = true
		}
	}
	styles, facialStyles := make(map[uint32]bool), make(map[uint32]bool)
	for _, row := range hairGeo {
		if row[1] == race && row[2] == gender {
			styles[row[3]] = true
		}
	}
	for _, row := range facialGeo {
		if row[0] == race && row[1] == gender {
			facialStyles[row[2]] = true
		}
	}
	skinFaces := make(map[[2]uint32]bool)
	hair := make(map[[3]uint32]bool)
	for _, row := range rows {
		if row[3] == 1 && skins[row[9]] {
			skinFaces[[2]uint32{row[9], row[8]}] = true
		}
		if row[3] == 3 && styles[row[8]] {
			for facial := range facialStyles {
				if len(facialTextures) == 0 || facialTextures[[2]uint32{facial, row[9]}] {
					hair[[3]uint32{row[8], row[9], facial}] = true
				}
			}
		}
	}
	if len(skinFaces) == 0 || len(hair) == 0 {
		return profile{}, fmt.Errorf("no valid appearance for race %d, gender %d, Death Knight %t", race, gender, dk)
	}
	p.SkinFaces = sortedPairs(skinFaces)
	for choice := range hair {
		p.Hair = append(p.Hair, choice)
	}
	slices.SortFunc(p.Hair, func(a, b [3]uint32) int { return slices.Compare(a[:], b[:]) })
	for _, choice := range p.SkinFaces {
		if choice[0] > 255 || choice[1] > 255 {
			return profile{}, fmt.Errorf("skin/face index exceeds one byte for race %d, gender %d", race, gender)
		}
	}
	for _, choice := range p.Hair {
		if choice[0] > 255 || choice[1] > 255 || choice[2] > 255 {
			return profile{}, fmt.Errorf("hair index exceeds one byte for race %d, gender %d", race, gender)
		}
	}
	return p, nil
}

func encodeCatalogue(info metadata, profiles []profile) ([]byte, error) {
	var out bytes.Buffer
	fmt.Fprintln(&out, "// Code generated by ./run charcatalog:generate; DO NOT EDIT.")
	fmt.Fprintf(&out, "// WoW 3.3.5a, build %d.\n", info.Build)
	fmt.Fprintln(&out, "// Source: "+info.Source)
	fmt.Fprintln(&out, "// AzerothCore revision: "+info.CoreRevision)
	fmt.Fprintln(&out, "// SHA-256:")
	fmt.Fprintln(&out, "// CharSections.dbc: "+info.SHA256.Sections)
	fmt.Fprintln(&out, "// CharHairGeosets.dbc: "+info.SHA256.Hair)
	fmt.Fprintln(&out, "// CharacterFacialHairStyles.dbc: "+info.SHA256.Facial)
	fmt.Fprintln(&out, "// playercreateinfo.sql: "+info.SHA256.Classes)
	fmt.Fprintln(&out, "\npackage char")
	fmt.Fprintf(&out, "\n// %d race/class pairs and %d appearance profiles.\n", len(info.RaceClasses), len(profiles))
	fmt.Fprintf(&out, "var catalogue = creationCatalogue{\nBuild: %d,\nRaceClasses: [][2]uint8{\n", info.Build)
	for _, pair := range info.RaceClasses {
		fmt.Fprintf(&out, "{%d, %d},\n", pair[0], pair[1])
	}
	fmt.Fprintln(&out, "},\nProfiles: []appearanceProfile{")
	for _, p := range profiles {
		fmt.Fprintf(&out, "{\nRace: %d, Gender: %d, DeathKnight: %t,\nSkinFaces: [][2]uint8{\n", p.Race, p.Gender, p.DeathKnight)
		for i, choice := range p.SkinFaces {
			fmt.Fprintf(&out, "{%d, %d},", choice[0], choice[1])
			if i%8 == 7 {
				fmt.Fprintln(&out)
			}
		}
		fmt.Fprintln(&out, "\n},\nHair: [][3]uint8{")
		for i, choice := range p.Hair {
			fmt.Fprintf(&out, "{%d, %d, %d},", choice[0], choice[1], choice[2])
			if i%8 == 7 {
				fmt.Fprintln(&out)
			}
		}
		fmt.Fprintln(&out, "\n},\n},")
	}
	fmt.Fprintln(&out, "},\n}")
	return format.Source(out.Bytes())
}

func writeCatalogue(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".catalogue-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0644); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
