package char

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hazim-j/agent-wow/pkg/opcode"
)

type appearanceProfile struct {
	Race        Race
	Gender      Gender
	DeathKnight bool
	SkinFaces   [][2]uint8 // skin, face
	Hair        [][3]uint8 // style, color, facial hair
}

type creationCatalogue struct {
	Build       int
	RaceClasses [][2]uint8
	Profiles    []appearanceProfile
}

func validateOptions(o CreateOptions) error {
	if o.Name != nil && (strings.TrimSpace(*o.Name) == "" || !utf8.ValidString(*o.Name) || strings.ContainsFunc(*o.Name, unicode.IsControl) || utf8.RuneCountInString(*o.Name) > 12) {
		return errors.New("name must contain 1–12 UTF-8 characters without control characters; omit --name to generate one")
	}
	if o.Race != nil {
		if _, ok := raceNames[*o.Race]; !ok {
			return fmt.Errorf("unknown race %d", *o.Race)
		}
	}
	if o.Class != nil {
		if _, ok := classNames[*o.Class]; !ok {
			return fmt.Errorf("unknown class %d", *o.Class)
		}
	}
	if o.Gender != nil {
		if _, ok := genderNames[*o.Gender]; !ok {
			return fmt.Errorf("unknown gender %d", *o.Gender)
		}
	}
	return nil
}

func (c *Client) random(n int) int {
	if c.choose != nil {
		return c.choose(n)
	}
	return rand.IntN(n)
}

func (c *Client) generatedName() string {
	// Use the maximum length and full ASCII alphabet to spread agents over a
	// large namespace. Reject entire candidates so all names satisfying these
	// structural rules have equal probability; repairing letters would bias it.
	for {
		var name [12]byte
		valid := true
		for i := range name {
			name[i] = 'a' + byte(c.random(26))
			if i >= 2 && name[i] == name[i-1] && name[i] == name[i-2] {
				valid = false
			}
		}
		// AzerothCore reserves names ending in "gm", ignoring case.
		if !valid || name[10] == 'g' && name[11] == 'm' {
			continue
		}
		name[0] -= 'a' - 'A'
		return string(name[:])
	}
}

func faction(r Race) int {
	switch r {
	case RaceHuman, RaceDwarf, RaceNightElf, RaceGnome, RaceDraenei:
		return 1
	case RaceOrc, RaceUndead, RaceTauren, RaceTroll, RaceBloodElf:
		return 2
	}
	return 0
}

func matches[T comparable](option *T, value T) bool { return option == nil || *option == value }

func (c *Client) resolve(o CreateOptions, characters []Character) (CreateResult, error) {
	if err := validateOptions(o); err != nil {
		return CreateResult{}, err
	}
	heroUnlocked := c.accountFlags&0x20000000 != 0
	hasHero, existingFaction := false, 0
	for _, ch := range characters {
		heroUnlocked = heroUnlocked || ch.Level >= 55
		hasHero = hasHero || ch.Class == ClassDeathKnight
		if existingFaction == 0 {
			existingFaction = faction(ch.Race)
		}
	}
	type candidate struct {
		race   Race
		class  Class
		gender Gender
		skins  [][2]uint8
		hair   [][3]uint8
	}
	var candidates []candidate
	for _, pair := range catalogue.RaceClasses {
		race, class := Race(pair[0]), Class(pair[1])
		if !matches(o.Race, race) || !matches(o.Class, class) {
			continue
		}
		if (race == RaceBloodElf || race == RaceDraenei) && c.expansion < 1 {
			continue
		}
		if class == ClassDeathKnight && (c.expansion < 2 || o.Class == nil && (!heroUnlocked || hasHero)) {
			continue
		}
		if o.Race == nil && (c.realm.Type == 1 || c.realm.Type == 8) && existingFaction != 0 && faction(race) != existingFaction {
			continue
		}
		for _, p := range catalogue.Profiles {
			if p.Race != race || p.DeathKnight != (class == ClassDeathKnight) || !matches(o.Gender, p.Gender) {
				continue
			}
			choice := candidate{race: race, class: class, gender: p.Gender}
			for _, sf := range p.SkinFaces {
				if matches(o.Skin, sf[0]) && matches(o.Face, sf[1]) {
					choice.skins = append(choice.skins, sf)
				}
			}
			if len(choice.skins) == 0 {
				continue
			}
			for _, hair := range p.Hair {
				if matches(o.HairStyle, hair[0]) && matches(o.HairColor, hair[1]) && matches(o.FacialHair, hair[2]) {
					choice.hair = append(choice.hair, hair)
				}
			}
			if len(choice.hair) != 0 {
				candidates = append(candidates, choice)
			}
		}
	}
	if len(candidates) == 0 {
		return CreateResult{}, fmt.Errorf("no valid character matches %s with this account's expansion and known realm restrictions", describeOptions(o))
	}
	choice := candidates[c.random(len(candidates))]
	sf, hair := choice.skins[c.random(len(choice.skins))], choice.hair[c.random(len(choice.hair))]
	name := ""
	if o.Name == nil {
		name = c.generatedName()
	} else {
		name = *o.Name
	}
	return CreateResult{Name: name, Race: choice.race, Class: choice.class, Gender: choice.gender,
		Appearance: Appearance{Skin: sf[0], Face: sf[1], HairStyle: hair[0], HairColor: hair[1], FacialHair: hair[2]}}, nil
}

func describeOptions(o CreateOptions) string {
	var parts []string
	if o.Race != nil {
		parts = append(parts, "race="+o.Race.String())
	}
	if o.Class != nil {
		parts = append(parts, "class="+o.Class.String())
	}
	if o.Gender != nil {
		parts = append(parts, "gender="+o.Gender.String())
	}
	for _, field := range []struct {
		name  string
		value *uint8
	}{
		{"skin", o.Skin}, {"face", o.Face}, {"hair-style", o.HairStyle}, {"hair-color", o.HairColor}, {"facial-hair", o.FacialHair},
	} {
		if field.value != nil {
			parts = append(parts, fmt.Sprintf("%s=%d", field.name, *field.value))
		}
	}
	if len(parts) == 0 {
		return "automatic creation options"
	}
	return strings.Join(parts, ", ")
}

// Create preserves explicit options and resolves all omitted fields together.
// Only definite generated-name rejections are retried, at most five attempts.
func (c *Client) Create(ctx context.Context, options CreateOptions) (CreateResult, error) {
	if err := validateOptions(options); err != nil {
		return CreateResult{}, err
	}
	var result CreateResult
	err := c.withContext(ctx, func() error {
		characters, err := c.list()
		if err != nil {
			return err
		}
		result, err = c.resolve(options, characters)
		if err != nil {
			return err
		}
		for attempt := 0; attempt < 5; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			a := result.Appearance
			body := append([]byte(result.Name), 0)
			body = append(body, byte(result.Race), byte(result.Class), byte(result.Gender), a.Skin, a.Face, a.HairStyle, a.HairColor, a.FacialHair, 0)
			err = c.mutate("create", opcode.CMSGCharCreate, opcode.SMSGCharCreate, 0x2f, body, result.Name, 0)
			if err == nil {
				return nil
			}
			var rejection *ServerError
			if options.Name != nil || attempt == 4 || !errors.As(err, &rejection) || rejection.Operation != "create" || !(rejection.Code == 0x32 || rejection.Code >= 0x58 && rejection.Code <= 0x67) {
				return err
			}
			result.Name = c.generatedName()
		}
		return err
	})
	if err != nil {
		return CreateResult{}, err
	}
	return result, nil
}
