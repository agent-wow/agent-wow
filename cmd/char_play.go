package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hazim-j/agent-wow/internal/config"
	"github.com/hazim-j/agent-wow/pkg/auth"
	"github.com/hazim-j/agent-wow/pkg/char"
	"github.com/hazim-j/agent-wow/pkg/world"
	"github.com/hazim-j/agent-wow/pkg/worldrpc"
	"github.com/spf13/cobra"
)

type playSession interface {
	worldrpc.Session
	Done() <-chan struct{}
	Err() error
	Close() error
}

func startCharacterPlay(ctx context.Context, realm auth.Realm, authSession *auth.Session, selector string, logger *slog.Logger) (playSession, error) {
	client, err := char.Dial(ctx, realm, authSession)
	if err != nil {
		return nil, err
	}
	defer client.Close() // EnterWorld transfers ownership; this is then harmless.
	characters, err := client.List(ctx)
	if err != nil {
		return nil, err
	}
	selected, err := findCharacter(characters, selector)
	if err != nil {
		return nil, err
	}
	return client.EnterWorld(ctx, selected.GUID, logger)
}

func (deps characterCommands) playCommand(timeout *time.Duration) *cobra.Command {
	return &cobra.Command{
		Use: "play <name|guid>", Short: "Enter the world and serve a local gameplay RPC API",
		Long: `Enter the world as the selected character. This creates a persistent connection with the world server and exposes a
local RPC API for gameplay actions.`,
		Example: `  agent-wow char play Arlen
  agent-wow char play 99
  curl -H 'Content-Type: application/json' http://127.0.0.1:8086/rpc -d '{"jsonrpc":"2.0","id":1,"method":"session.getState"}'
  curl -H 'Content-Type: application/json' http://127.0.0.1:8086/rpc -d '{"jsonrpc":"2.0","id":2,"method":"session.logout"}'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if strings.TrimSpace(args[0]) == "" {
				return errors.New("character name or GUID must not be empty")
			}
			if *timeout <= 0 {
				return errors.New("--timeout must be greater than zero")
			}
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: config.Get().LogLevel}))
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			startup, cancel := context.WithTimeout(ctx, *timeout)
			defer cancel()
			listener, err := worldrpc.Listen(startup, worldRPCAddress())
			if err != nil {
				return err
			}
			defer listener.Close()
			realm, saved, err := characterTarget(startup)
			if err != nil {
				return err
			}
			session, err := deps.play(startup, realm, saved, args[0], logger)
			if err != nil {
				return err
			}
			cancel() // Startup cancellation cannot affect the transferred connection.
			defer session.Close()
			return servePlay(ctx, listener, session, logger)
		},
	}
}

func worldRPCAddress() string {
	server := config.Get().WorldRPC
	return net.JoinHostPort(server.Host, strconv.Itoa(server.Port))
}

func servePlay(ctx context.Context, listener net.Listener, session playSession, logger *slog.Logger) error {
	select {
	case <-session.Done():
		if err := session.Err(); err != nil {
			return err
		}
		return errors.New("gameplay session ended before RPC startup")
	default:
	}
	server := &http.Server{Handler: worldrpc.Handler(session), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 8 << 10, ErrorLog: slog.NewLogLogger(logger.With("component", "rpc").Handler(), slog.LevelError)}
	serving := make(chan error, 1)
	stopped := make(chan struct{})
	go func() { defer close(stopped); serving <- server.Serve(listener) }()
	defer func() { _ = server.Close(); <-stopped }()
	state := session.Snapshot()
	if state.Status == world.Closed || state.Status == world.Failed {
		return errors.New("gameplay session ended before RPC readiness")
	}
	logger.Info("world session ready",
		"event", "ready",
		"rpc_url", "http://"+listener.Addr().String()+"/rpc",
		"pid", os.Getpid(),
		slog.Group("realm", "id", state.Realm.ID, "name", state.Realm.Name),
		slog.Group("character", "guid", strconv.FormatUint(state.Character.GUID, 10), "name", state.Character.Name),
	)
	var result error
	select {
	case <-ctx.Done():
		logout, cancel := context.WithTimeout(context.Background(), 31*time.Second)
		result = session.Logout(logout)
		cancel()
		if result != nil {
			_ = session.Close()
		}
	case <-session.Done():
		result = session.Err()
	case err := <-serving:
		result = fmt.Errorf("RPC server stopped: %w", err)
		_ = session.Close()
	}
	// Shutdown closes the listener but waits for in-flight handlers, ensuring a
	// confirmed logout response is written before the process exits.
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(drain); err != nil {
		result = errors.Join(result, fmt.Errorf("drain RPC responses: %w", err))
	}
	return result
}
