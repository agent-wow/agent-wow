package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agent-wow/agent-wow/pkg/char"
	"github.com/spf13/cobra"
)

func (deps characterCommands) createCommand(timeout *time.Duration) *cobra.Command {
	var name, race, class, gender string
	var skin, face, hairStyle, hairColor, facialHair uint8
	command := &cobra.Command{Use: "create", Short: "Create a character, randomizing all omitted options",
		Long:    "Create a character on the selected realm. Omitted options are randomized together using stock 3.3.5a compatibility data. Explicit options, including zero, are preserved.\n\nRace names: human, orc, dwarf, night-elf, undead, tauren, gnome, troll, blood-elf, draenei.\nClass names: warrior, paladin, hunter, rogue, priest, death-knight, shaman, mage, warlock, druid.\nGender names: male, female. Numeric protocol IDs are also accepted.",
		Example: "  agent-wow char create\n  agent-wow char create --name Arlen --race human --class warrior\n  agent-wow char create --class mage --gender female --hair-color 0",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			o := char.CreateOptions{}
			if cmd.Flags().Changed("name") {
				if strings.TrimSpace(name) == "" {
					return errors.New("--name must not be empty; omit it to generate a name")
				}
				o.Name = &name
			}
			if cmd.Flags().Changed("race") {
				value, err := char.ParseRace(race)
				if err != nil {
					return err
				}
				o.Race = &value
			}
			if cmd.Flags().Changed("class") {
				value, err := char.ParseClass(class)
				if err != nil {
					return err
				}
				o.Class = &value
			}
			if cmd.Flags().Changed("gender") {
				value, err := char.ParseGender(gender)
				if err != nil {
					return err
				}
				o.Gender = &value
			}
			for _, field := range []struct {
				name        string
				value       *uint8
				destination **uint8
			}{
				{"skin", &skin, &o.Skin}, {"face", &face, &o.Face}, {"hair-style", &hairStyle, &o.HairStyle}, {"hair-color", &hairColor, &o.HairColor}, {"facial-hair", &facialHair, &o.FacialHair},
			} {
				if cmd.Flags().Changed(field.name) {
					*field.destination = field.value
				}
			}
			ctx, cancel, err := characterContext(cmd, *timeout)
			if err != nil {
				return err
			}
			defer cancel()
			realm, session, err := characterTarget(ctx)
			if err != nil {
				return err
			}
			client, err := deps.dial(ctx, realm, session)
			if err != nil {
				return err
			}
			defer client.Close()
			created, err := client.Create(ctx, o)
			if err != nil {
				return err
			}
			a := created.Appearance
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Created character: %s\nRealm: %s (ID: %d)\nRace: %s\nClass: %s\nGender: %s\nSkin: %d\nFace: %d\nHair style: %d\nHair color: %d\nFacial hair: %d\n",
				created.Name, realm.Name, realm.ID, created.Race, created.Class, created.Gender, a.Skin, a.Face, a.HairStyle, a.HairColor, a.FacialHair)
			return err
		}}
	flags := command.Flags()
	flags.StringVar(&name, "name", "", "Character name (randomized when omitted)")
	flags.StringVar(&race, "race", "", "Race name or ID (randomized when omitted)")
	flags.StringVar(&class, "class", "", "Class name or ID (randomized when omitted)")
	flags.StringVar(&gender, "gender", "", "Gender name or ID: male/0, female/1 (randomized when omitted)")
	flags.Uint8Var(&skin, "skin", 0, "Skin index (randomized when omitted)")
	flags.Uint8Var(&face, "face", 0, "Face index (randomized when omitted)")
	flags.Uint8Var(&hairStyle, "hair-style", 0, "Hair style index (randomized when omitted)")
	flags.Uint8Var(&hairColor, "hair-color", 0, "Hair color index (randomized when omitted)")
	flags.Uint8Var(&facialHair, "facial-hair", 0, "Facial hair or feature index (randomized when omitted)")
	return command
}
