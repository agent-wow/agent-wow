package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	bridgev1 "github.com/agent-wow/agent-wow/examples/packet-bridge/api"
	modv1 "github.com/agent-wow/agent-wow/pkg/modules/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("packet bridge stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	moduleSocket := os.Getenv("AGENT_WOW_MODULE_SOCKET")
	sessionSocket := os.Getenv("AGENT_WOW_SESSION_SOCKET")
	if moduleSocket == "" || sessionSocket == "" {
		return fmt.Errorf("AGENT_WOW_MODULE_SOCKET and AGENT_WOW_SESSION_SOCKET must be set")
	}
	listener, err := net.Listen("unix", moduleSocket)
	if err != nil {
		return err
	}
	defer listener.Close()
	// The host and module use the same private socket directory.
	if err := os.Chmod(moduleSocket, 0666); err != nil {
		return err
	}
	conn, err := grpc.NewClient("unix://"+sessionSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDisableRetry(),
		grpc.WithDefaultCallOptions(grpc.MaxRetryRPCBufferSize(0)),
	)
	if err != nil {
		return err
	}
	defer conn.Close()
	server := grpc.NewServer()
	defer server.Stop()
	bridgev1.RegisterPacketBridgeServer(server, &bridge{session: modv1.NewSessionClient(conn)})
	healthy := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthy)
	healthy.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	stopServer := context.AfterFunc(ctx, server.Stop)
	defer stopServer()
	if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return err
	}
	return nil
}
