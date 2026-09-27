package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	realmstore "github.com/hazim-j/agent-wow/internal/realm"
	"github.com/hazim-j/agent-wow/pkg/auth"
	realmtypes "github.com/hazim-j/agent-wow/pkg/realm"
	"github.com/spf13/cobra"
)

func newRealmCommand() *cobra.Command {
	var timeout time.Duration
	command := &cobra.Command{
		Use:   "realm",
		Short: "List, select and check realms",
		Long:  "Manage realms using the saved authenticated session. The selected realm is saved in config_dir/realm.json.",
		Args:  cobra.NoArgs,
	}
	command.PersistentFlags().DurationVar(&timeout, "timeout", 10*time.Second, "Timeout for fetching realms from the authserver")
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List realms and mark the current selection",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			realms, err := fetchRealms(cmd, timeout)
			if err != nil {
				return err
			}
			if len(realms) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "No realms available.")
				return err
			}
			selected := realmstore.Get()
			out := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(out, "SELECTED\tID\tNAME\tTYPE\tSTATUS\tCHARACTERS\tPOPULATION\tADDRESS")
			for _, realm := range realms {
				marker := ""
				if selected != nil && selected.ID == realm.ID {
					marker = "*"
				}
				fmt.Fprintf(out, "%s\t%d\t%s\t%s\t%s\t%d\t%.2f\t%s\n", marker, realm.ID, realm.Name, realmtypes.FormatType(realm.Type), realm.Status(), realm.Characters, realm.Population, realm.Address)
			}
			return out.Flush()
		},
	}, &cobra.Command{
		Use:     "set <id|name>",
		Short:   "Select an available realm by ID or exact name",
		Example: "  agent-wow realm set 1\n  agent-wow realm set \"AzerothCore\"",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if strings.TrimSpace(args[0]) == "" {
				return errors.New("realm ID or name must not be empty")
			}
			realms, err := fetchRealms(cmd, timeout)
			if err != nil {
				return err
			}
			realm, err := findRealm(realms, args[0])
			if err != nil {
				return err
			}
			if !realm.Selectable() {
				return fmt.Errorf("realm %q is %s; selection unchanged", realm.Name, realm.Status())
			}
			if err := saveRealm(realm); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Selected realm: %s (ID: %d)\nAddress: %s\nSaved to %s.\n", realm.Name, realm.ID, realm.Address, realmstore.Path())
			return err
		},
	}, &cobra.Command{
		Use:   "status",
		Short: "Show the selected realm and its current availability",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if timeout <= 0 {
				return errors.New("--timeout must be greater than zero")
			}
			selected := realmstore.Get()
			if selected == nil {
				return errors.New("no realm selected; run 'agent-wow auth login' or 'agent-wow realm set <id|name>'")
			}
			realms, err := fetchRealms(cmd, timeout)
			if err != nil {
				return err
			}
			for _, realm := range realms {
				if realm.ID == selected.ID {
					_, err := fmt.Fprintf(cmd.OutOrStdout(), "Selected realm: %s (ID: %d)\nStatus: %s\nType: %s\nCharacters: %d\nPopulation: %.2f\nAddress: %s\nAuthserver: %s\n", realm.Name, realm.ID, realm.Status(), realmtypes.FormatType(realm.Type), realm.Characters, realm.Population, realm.Address, authServerAddress())
					return err
				}
			}
			return fmt.Errorf("selected realm %q (ID: %d) is no longer listed; run 'agent-wow realm list' and 'agent-wow realm set <id|name>'", selected.Name, selected.ID)
		},
	})
	return command
}

func fetchRealms(cmd *cobra.Command, timeout time.Duration) ([]auth.Realm, error) {
	if timeout <= 0 {
		return nil, errors.New("--timeout must be greater than zero")
	}
	session, err := loadSavedSession()
	if err != nil {
		return nil, err
	}
	client, err := auth.NewClient(authServerAddress())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	realms, err := client.ListRealms(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("%w; if the saved session is invalid, run 'agent-wow auth login'", err)
	}
	return realms, nil
}

func findRealm(realms []auth.Realm, selector string) (auth.Realm, error) {
	// Numeric IDs take precedence over names. Otherwise match the complete name,
	// ignoring case, and require an ID when names are ambiguous.
	if id, err := strconv.ParseUint(selector, 10, 8); err == nil {
		for _, realm := range realms {
			if realm.ID == uint8(id) {
				return realm, nil
			}
		}
	}
	var matches []auth.Realm
	for _, realm := range realms {
		if strings.EqualFold(realm.Name, selector) {
			matches = append(matches, realm)
		}
	}
	if len(matches) > 1 {
		return auth.Realm{}, fmt.Errorf("realm name %q is ambiguous; select by ID", selector)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return auth.Realm{}, fmt.Errorf("realm %q not found; run 'agent-wow realm list'", selector)
}

func saveRealm(realm auth.Realm) error {
	return realmstore.Save(realmstore.Selection{
		ID: realm.ID, Name: realm.Name, Address: realm.Address,
	})
}

// selectLoginRealm keeps an existing choice, including when it is unavailable.
// On first login, choose the first selectable realm in the server's list.
func selectLoginRealm(realms []auth.Realm) error {
	selected := realmstore.Get()
	for _, realm := range realms {
		if selected != nil && realm.ID != selected.ID {
			continue
		}
		if !realm.Selectable() {
			if selected != nil {
				return fmt.Errorf("selected realm %q is %s; use 'agent-wow realm list' and 'agent-wow realm set <id|name>' to change realms", realm.Name, realm.Status())
			}
			continue
		}
		return saveRealm(realm)
	}
	if selected != nil {
		return fmt.Errorf("selected realm %q (ID: %d) is no longer listed; use 'agent-wow realm set <id|name>' to change realms", selected.Name, selected.ID)
	}
	return errors.New("no available realms to select; run 'agent-wow realm list' to check availability")
}

func init() { rootCmd.AddCommand(newRealmCommand()) }
