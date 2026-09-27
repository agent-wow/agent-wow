package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hazim-j/agent-wow/pkg/auth"
	"github.com/hazim-j/agent-wow/pkg/char"
	"github.com/hazim-j/agent-wow/pkg/maps"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type characterClient interface {
	Close() error
	List(context.Context) ([]char.Character, error)
	Create(context.Context, char.CreateOptions) (char.CreateResult, error)
	Delete(context.Context, char.GUID) error
}

type characterCommands struct {
	dial       func(context.Context, auth.Realm, *auth.Session) (characterClient, error)
	isTerminal func(io.Reader) bool
}

func newCharCommand() *cobra.Command {
	return characterCommands{
		dial: func(ctx context.Context, realm auth.Realm, session *auth.Session) (characterClient, error) {
			return char.Dial(ctx, realm, session)
		},
		isTerminal: func(input io.Reader) bool {
			file, ok := input.(interface{ Fd() uintptr })
			return ok && term.IsTerminal(int(file.Fd()))
		},
	}.command()
}

func (deps characterCommands) command() *cobra.Command {
	var timeout time.Duration
	command := &cobra.Command{Use: "char", Short: "List, create and delete characters on the selected realm", Args: cobra.NoArgs}
	command.PersistentFlags().DurationVar(&timeout, "timeout", 10*time.Second, "Timeout per network phase (excludes confirmation prompts)")
	command.AddCommand(deps.listCommand(&timeout), deps.createCommand(&timeout), deps.deleteCommand(&timeout))
	return command
}

func characterTarget(ctx context.Context) (auth.Realm, *auth.Session, error) {
	selected, err := loadSelectedRealm()
	if err != nil {
		return auth.Realm{}, nil, err
	}
	if selected == nil {
		return auth.Realm{}, nil, errors.New("no realm selected; run 'agent-wow auth login' or 'agent-wow realm set <id|name>'")
	}
	session, err := loadSavedSession()
	if err != nil {
		return auth.Realm{}, nil, err
	}
	client, err := auth.NewClient(authServerAddress())
	if err != nil {
		return auth.Realm{}, nil, err
	}
	realms, err := client.ListRealms(ctx, session)
	if err != nil {
		return auth.Realm{}, nil, fmt.Errorf("fetch selected realm: %w; if the saved session is invalid, run 'agent-wow auth login'", err)
	}
	for _, realm := range realms {
		if realm.ID != selected.ID {
			continue
		}
		if !realm.Selectable() {
			return auth.Realm{}, nil, fmt.Errorf("selected realm %q is %s", realm.Name, realm.Status())
		}
		return realm, session, nil
	}
	return auth.Realm{}, nil, fmt.Errorf("selected realm %q is no longer listed; run 'agent-wow realm list' and 'agent-wow realm set <id|name>'", selected.Name)
}

func characterContext(cmd *cobra.Command, timeout time.Duration) (context.Context, context.CancelFunc, error) {
	cmd.SilenceUsage = true
	if timeout <= 0 {
		return nil, nil, errors.New("--timeout must be greater than zero")
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	return ctx, cancel, nil
}

func (deps characterCommands) listCommand(timeout *time.Duration) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{Use: "list", Short: "List characters on the selected realm", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			characters, err := client.List(ctx)
			if err != nil {
				return err
			}
			if jsonOutput {
				return writeCharacterJSON(cmd.OutOrStdout(), realm, characters)
			}
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "Realm: %s (ID: %d)\n", realm.Name, realm.ID); err != nil {
				return err
			}
			if len(characters) == 0 {
				_, err = fmt.Fprintln(out, "No characters available.")
				return err
			}
			table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(table, "GUID\tNAME\tRACE\tCLASS\tGENDER\tLEVEL\tZONE\tMAP")
			for _, ch := range characters {
				fmt.Fprintf(table, "%d\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n", ch.GUID, ch.Name, ch.Race, ch.Class, ch.Gender, ch.Level, maps.ZoneName(ch.ZoneID), maps.MapName(ch.MapID))
			}
			return table.Flush()
		}}
	command.Flags().BoolVar(&jsonOutput, "json", false, "Output characters and realm identity as JSON")
	return command
}

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

func (deps characterCommands) deleteCommand(timeout *time.Duration) *cobra.Command {
	var yes bool
	command := &cobra.Command{Use: "delete <name|guid>", Short: "Delete an account-owned character, with confirmation", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if strings.TrimSpace(args[0]) == "" {
				return errors.New("character name or GUID must not be empty")
			}
			if !yes && !deps.isTerminal(cmd.InOrStdin()) {
				return errors.New("noninteractive deletion requires --yes")
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
			defer func() { _ = client.Close() }()
			characters, err := client.List(ctx)
			if err != nil {
				return err
			}
			target, err := findCharacter(characters, args[0])
			if err != nil {
				return err
			}
			if !yes {
				_ = client.Close()
				cancel()
				prompt := fmt.Sprintf("Delete %s (GUID: %d) on %s (realm ID: %d)? [y/N]: ", target.Name, target.GUID, realm.Name, realm.ID)
				answer, err := promptLine(cmd.InOrStdin(), cmd.ErrOrStderr(), prompt)
				if err != nil && !errors.Is(err, io.EOF) {
					return fmt.Errorf("read deletion confirmation: %w", err)
				}
				if !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), "Deletion cancelled.")
					return err
				}
				ctx, cancel = context.WithTimeout(cmd.Context(), *timeout)
				defer cancel()
				reconnected, err := deps.dial(ctx, realm, session)
				if err != nil {
					return err
				}
				client = reconnected
				characters, err = client.List(ctx)
				if err != nil {
					return err
				}
				current, err := findCharacter(characters, strconv.FormatUint(uint64(target.GUID), 10))
				if err != nil || current.Name != target.Name {
					return errors.New("character changed or disappeared after confirmation; run 'agent-wow char list' and confirm again")
				}
			}
			if err := client.Delete(ctx, target.GUID); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Deleted character: %s (GUID: %d)\nRealm: %s (ID: %d)\n", target.Name, target.GUID, realm.Name, realm.ID)
			return err
		}}
	command.Flags().BoolVar(&yes, "yes", false, "Confirm deletion without prompting")
	return command
}

func findCharacter(characters []char.Character, selector string) (char.Character, error) {
	base := 10
	numeric := selector
	if strings.HasPrefix(strings.ToLower(numeric), "0x") {
		base, numeric = 16, numeric[2:]
	}
	if guid, err := strconv.ParseUint(numeric, base, 64); err == nil {
		for _, ch := range characters {
			if uint64(ch.GUID) == guid {
				return ch, nil
			}
		}
		return char.Character{}, fmt.Errorf("character GUID %q not found; run 'agent-wow char list'", selector)
	}
	var matches []char.Character
	for _, ch := range characters {
		if strings.EqualFold(ch.Name, selector) {
			matches = append(matches, ch)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return char.Character{}, fmt.Errorf("character name %q is ambiguous; use its GUID", selector)
	}
	return char.Character{}, fmt.Errorf("character %q not found; run 'agent-wow char list'", selector)
}

func init() { rootCmd.AddCommand(newCharCommand()) }
