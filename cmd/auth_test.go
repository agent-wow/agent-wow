package cmd

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hazim-j/agent-wow/internal/config"
	"github.com/hazim-j/agent-wow/internal/credentials"
)

func TestAuthLoginInputValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing file", []string{"login"}, "run 'agent-wow auth init'"},
		{"invalid timeout", []string{"login", "--timeout", "0"}, "--timeout must be greater"},
		{"removed username flag", []string{"login", "--username", "player"}, "unknown flag"},
		{"removed password stdin flag", []string{"login", "--password-stdin"}, "unknown flag"},
		{"positional argument", []string{"login", "player"}, "unknown command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initTestConfig(t)
			// Legacy environment variables must not supply credentials.
			t.Setenv("AGENT_WOW_USERNAME", "player")
			t.Setenv("AGENT_WOW_PASSWORD", "secret")
			cmd := newAuthCommand()
			cmd.SetArgs(tc.args)
			cmd.SetIn(strings.NewReader(""))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestAuthInit(t *testing.T) {
	initTestConfig(t)
	cmd := newAuthCommand()
	cmd.SetArgs([]string{"init"})
	cmd.SetIn(strings.NewReader("player\r\n secret with spaces \r\n"))
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	creds, err := credentials.Read(config.Get().AuthFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Username != "player" || creds.Password != " secret with spaces " {
		t.Fatal("credentials were not saved correctly")
	}
	for _, want := range []string{"Username: ", "Password: ", "Credentials saved", "agent-wow auth login"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("missing %q in command output", want)
		}
	}
	if strings.Contains(output.String(), creds.Password) {
		t.Fatal("password appeared in command output")
	}
}

func TestAuthInitInvalidInputPreservesFile(t *testing.T) {
	initTestConfig(t)
	path := config.Get().AuthFilePath
	original := credentials.Credentials{Username: "player", Password: "original"}
	if err := credentials.Write(path, original); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"", "player\n", "\npassword\n", "player\n\n"} {
		cmd := newAuthCommand()
		cmd.SetArgs([]string{"init"})
		cmd.SetIn(strings.NewReader(input))
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err == nil {
			t.Fatal("accepted incomplete or empty credentials")
		}
		got, err := credentials.Read(path)
		if err != nil || got != original {
			t.Fatal("invalid input replaced the existing auth file")
		}
	}
}

func TestPromptLine(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"secret\nignored\n", "secret"},
		{"secret\r\n", "secret"},
		{"secret", "secret"},
		{" spaces \n", " spaces "},
		{"\n", ""},
	} {
		got, err := promptLine(strings.NewReader(tc.input), io.Discard, "Password: ")
		if err != nil || got != tc.want {
			t.Errorf("unexpected line input result: %v", err)
		}
	}
	if _, err := promptLine(strings.NewReader(strings.Repeat("x", 4097)), io.Discard, ""); err == nil {
		t.Fatal("accepted unbounded input")
	}
	if _, err := promptLine(errorReader{}, io.Discard, ""); err == nil {
		t.Fatal("ignored stdin read error")
	}
}

func TestAuthLoginUsesFileAndConfiguredServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_WOW_AUTHSERVER_HOST", host)
	t.Setenv("AGENT_WOW_AUTHSERVER_PORT", port)
	t.Setenv("AGENT_WOW_USERNAME", "ignored")
	t.Setenv("AGENT_WOW_PASSWORD", "ignored")
	initTestConfig(t)
	if err := credentials.Write(config.Get().AuthFilePath, credentials.Credentials{Username: "player", Password: "secret"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		request := make([]byte, 40)
		if _, err := io.ReadFull(conn, request); err != nil {
			done <- err
			return
		}
		if string(request[34:]) != "PLAYER" {
			done <- errors.New("login did not use the account from the auth file")
			return
		}
		_, err = conn.Write([]byte{0, 0, 4})
		done <- err
	}()
	cmd := newAuthCommand()
	cmd.SetArgs([]string{"login", "--timeout", "1s"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "incorrect account name or password") {
		t.Errorf("expected server rejection, got %v", err)
	}
	_ = listener.Close()
	if err := <-done; err != nil {
		t.Error(err)
	}
	if strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "Authenticated") {
		t.Fatal("failed login leaked the password or reported success")
	}
}

func initTestConfig(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AGENT_WOW_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("AGENT_WOW_AUTH_FILE_PATH", "")
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.Init(configPath); err != nil {
		t.Fatal(err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("stdin failed") }
