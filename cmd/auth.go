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
	"github.com/hazim-j/agent-wow/pkg/auth"
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
	command.AddCommand(newAuthInitCommand(), newAuthLoginCommand())
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
		Long:  "Authenticate with the AzerothCore authserver to connect to the worldserver",
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
			server := cfg.AuthServer
			address := net.JoinHostPort(server.Host, strconv.Itoa(server.Port))
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			session, err := auth.Authenticate(ctx, address, creds.Username, creds.Password)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Authenticated as %s with %s.\n", session.Username, address)
			return err
		},
	}
	command.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "Timeout for the authserver connection and login")
	return command
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
