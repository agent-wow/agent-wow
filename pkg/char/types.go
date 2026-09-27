// Package char implements WoW 3.3.5a (build 12340) character selection and
// management. It uses the public realm protocol and never enters the world.
package char

import (
	"fmt"
	"strconv"
	"strings"
)

type GUID uint64
type Race uint8
type Class uint8
type Gender uint8

const (
	RaceHuman    Race = 1
	RaceOrc      Race = 2
	RaceDwarf    Race = 3
	RaceNightElf Race = 4
	RaceUndead   Race = 5
	RaceTauren   Race = 6
	RaceGnome    Race = 7
	RaceTroll    Race = 8
	RaceBloodElf Race = 10
	RaceDraenei  Race = 11
)

const (
	ClassWarrior     Class = 1
	ClassPaladin     Class = 2
	ClassHunter      Class = 3
	ClassRogue       Class = 4
	ClassPriest      Class = 5
	ClassDeathKnight Class = 6
	ClassShaman      Class = 7
	ClassMage        Class = 8
	ClassWarlock     Class = 9
	ClassDruid       Class = 11
)

const (
	GenderMale   Gender = 0
	GenderFemale Gender = 1
)

var raceNames = map[Race]string{1: "Human", 2: "Orc", 3: "Dwarf", 4: "Night Elf", 5: "Undead", 6: "Tauren", 7: "Gnome", 8: "Troll", 10: "Blood Elf", 11: "Draenei"}
var classNames = map[Class]string{1: "Warrior", 2: "Paladin", 3: "Hunter", 4: "Rogue", 5: "Priest", 6: "Death Knight", 7: "Shaman", 8: "Mage", 9: "Warlock", 11: "Druid"}
var genderNames = map[Gender]string{0: "Male", 1: "Female"}

func (r Race) String() string   { return enumName(r, raceNames) }
func (c Class) String() string  { return enumName(c, classNames) }
func (g Gender) String() string { return enumName(g, genderNames) }

func ParseRace(value string) (Race, error)     { return parseEnum(value, "race", raceNames) }
func ParseClass(value string) (Class, error)   { return parseEnum(value, "class", classNames) }
func ParseGender(value string) (Gender, error) { return parseEnum(value, "gender", genderNames) }

func enumName[T ~uint8](value T, names map[T]string) string {
	if name, ok := names[value]; ok {
		return name
	}
	return strconv.Itoa(int(value))
}

func parseEnum[T ~uint8](value, kind string, names map[T]string) (T, error) {
	normalize := func(s string) string {
		return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(strings.TrimSpace(s)))
	}
	if n, err := strconv.ParseUint(value, 10, 8); err == nil {
		if _, ok := names[T(n)]; ok {
			return T(n), nil
		}
	}
	for id, name := range names {
		if normalize(value) == normalize(name) {
			return id, nil
		}
	}
	return 0, fmt.Errorf("unknown %s %q", kind, value)
}

// CreateOptions distinguishes omitted values (nil) from explicit zero values.
type CreateOptions struct {
	Name       *string
	Race       *Race
	Class      *Class
	Gender     *Gender
	Skin       *uint8
	Face       *uint8
	HairStyle  *uint8
	HairColor  *uint8
	FacialHair *uint8
}

type Appearance struct {
	Skin       uint8
	Face       uint8
	HairStyle  uint8
	HairColor  uint8
	FacialHair uint8
}

// CreateResult contains the confirmed creation settings. The server does not
// return a GUID in its creation response; use List to retrieve it.
type CreateResult struct {
	Name       string
	Race       Race
	Class      Class
	Gender     Gender
	Appearance Appearance
}

type Character struct {
	GUID       GUID
	Name       string
	Race       Race
	Class      Class
	Gender     Gender
	Level      uint8
	ZoneID     uint32
	MapID      uint32
	Appearance Appearance
}
