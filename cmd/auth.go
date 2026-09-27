package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hazim-j/agent-wow/internal/config"
	"github.com/hazim-j/agent-wow/internal/credentials"
	realmstore "github.com/hazim-j/agent-wow/internal/realm"
	"github.com/hazim-j/agent-wow/pkg/account"
	"github.com/hazim-j/agent-wow/pkg/auth"
	"github.com/hazim-j/agent-wow/pkg/database"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAuthCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "auth",
		Short: "Commands to authenticate with the authserver",
		Long:  `Commands to authenticate with the AzerothCore authserver`,
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newAuthInitCommand(), newAuthLoginCommand(), newAuthStatusCommand())
	return command
}

func newAuthInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup for auth.json file",
		Long:  `Interactive setup for auth.json file with account username and password`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			creds, err := promptCredentials(cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			path := config.Get().AuthFilePath
			if err := credentials.Write(path, creds); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Credentials saved to %s. Run 'agent-wow auth login' to authenticate.\n", path)
			return err
		},
	}
}

func newAuthLoginCommand() *cobra.Command {
	var timeout time.Duration
	command := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with the authserver",
		Long:  "Authenticate with the AzerothCore authserver and restore the selected realm. On first login, automatically select the first available realm and save it in config_dir/realm.json.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if timeout <= 0 {
				return errors.New("--timeout must be greater than zero")
			}
			cfg := config.Get()
			creds, err := credentials.Read(cfg.AuthFilePath)
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("auth file %q not found; run 'agent-wow auth init' to create it", cfg.AuthFilePath)
			}
			if err != nil {
				return err
			}
			address := authServerAddress()
			authClient, err := auth.NewClient(address)
			if err != nil {
				return err
			}
			dbClient, err := database.NewClient(cfg.DataDir)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			session, err := authClient.Login(ctx, creds.Username, creds.Password)
			if err != nil {
				return errors.Join(err, dbClient.Close())
			}
			if err := errors.Join(dbClient.SaveSession(session), dbClient.Close()); err != nil {
				return fmt.Errorf("persist authenticated session: %w", err)
			}
			realms, err := authClient.ListRealms(ctx, session)
			if err == nil {
				err = selectLoginRealm(realms)
			}
			if err != nil {
				return fmt.Errorf("authenticated session saved, but realm selection failed: %w", err)
			}
			return printAuthSummary(cmd.OutOrStdout(), "Authentication success.", session, address)
		},
	}
	command.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "Timeout for login and realm selection")
	return command
}

func newAuthStatusCommand() *cobra.Command {
	var timeout time.Duration
	command := &cobra.Command{
		Use:   "status",
		Short: "Check whether the saved session is still valid",
		Long:  "Check the database session with the authserver using its saved key, without logging in again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if timeout <= 0 {
				return errors.New("--timeout must be greater than zero")
			}
			session, err := loadSavedSession()
			if err != nil {
				return err
			}
			address := authServerAddress()
			authClient, err := auth.NewClient(address)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			if err := authClient.CheckSession(ctx, session); err != nil {
				return fmt.Errorf("could not validate saved session for %s: %w", session.Username, err)
			}
			return printAuthSummary(cmd.OutOrStdout(), "Session valid.", session, address)
		},
	}
	command.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "Timeout for checking the session with the authserver")
	return command
}

func loadSavedSession() (*auth.Session, error) {
	cfg := config.Get()
	dbClient, err := database.NewClient(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	session, err := dbClient.GetSession()
	err = errors.Join(err, dbClient.Close())
	if errors.Is(err, database.ErrSessionNotFound) {
		return nil, fmt.Errorf("no saved session; run 'agent-wow auth login' to authenticate: %w", err)
	}
	if err != nil {
		return nil, err
	}
	return session, nil
}

func authServerAddress() string {
	server := config.Get().AuthServer
	return net.JoinHostPort(server.Host, strconv.Itoa(server.Port))
}

func printAuthSummary(out io.Writer, message string, session *auth.Session, address string) error {
	selectedRealm, realmAddress := "none", "none"
	if selected := realmstore.Get(); selected != nil {
		selectedRealm = fmt.Sprintf("%s (ID: %d)", selected.Name, selected.ID)
		realmAddress = selected.Address
	}
	_, err := fmt.Fprintf(out, "%s\nUsername: %s\nAccount flags: %s\nAuthserver: %s\nSelected realm: %s\nAddress: %s\n",
		message, session.Username, account.FormatFlags(session.AccountFlags), address, selectedRealm, realmAddress)
	return err
}

func promptCredentials(input io.Reader, output io.Writer) (credentials.Credentials, error) {
	var username, password string
	var err error
	if file, ok := input.(interface{ Fd() uintptr }); ok && term.IsTerminal(int(file.Fd())) {
		// Raw mode lets the prompt handle Ctrl-C and restore echo on return.
		fd := int(file.Fd())
		state, setupErr := term.MakeRaw(fd)
		if setupErr != nil {
			return credentials.Credentials{}, fmt.Errorf("prepare credential prompt: %w", setupErr)
		}
		defer term.Restore(fd, state)
		terminal := term.NewTerminal(struct {
			io.Reader
			io.Writer
		}{input, output}, "Username: ")
		username, err = terminal.ReadLine()
		if err == nil {
			password, err = terminal.ReadPassword("Password: ")
		}
	} else {
		username, err = promptLine(input, output, "Username: ")
		if err == nil {
			password, err = promptLine(input, output, "Password: ")
		}
	}
	if err != nil {
		return credentials.Credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	return credentials.Credentials{Username: strings.TrimSpace(username), Password: password}, nil
}

func promptLine(input io.Reader, output io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(output, prompt); err != nil {
		return "", err
	}
	// Read one line without consuming bytes belonging to the next prompt.
	var line []byte
	var next [1]byte
	for len(line) <= 4096 {
		if _, err := io.ReadFull(input, next[:]); err != nil {
			if errors.Is(err, io.EOF) && len(line) != 0 {
				return string(line), nil
			}
			return "", err
		}
		if next[0] == '\n' {
			return strings.TrimSuffix(string(line), "\r"), nil
		}
		line = append(line, next[0])
	}
	return "", errors.New("credential input is too long")
}

func init() {
	rootCmd.AddCommand(newAuthCommand())
}
