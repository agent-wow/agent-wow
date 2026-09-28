package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

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
