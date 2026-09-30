package modrt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func (m *Manager) Done() <-chan struct{} { return m.done }

func (m *Manager) Err() error { m.mu.Lock(); defer m.mu.Unlock(); return m.err }

func (m *Manager) fail(name string, err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing || m.err != nil {
		return
	}
	m.err = fmt.Errorf("module %s: %w", name, err)
	m.failOnce.Do(func() { close(m.done) })
	m.cancel()
}

func (m *Manager) waitHealthy(ctx context.Context, inst *instance) error {
	client := grpc_health_v1.NewHealthClient(inst.conn)
	for {
		check, cancel := context.WithTimeout(ctx, m.timeHealth)
		r, err := client.Check(check, &grpc_health_v1.HealthCheckRequest{})
		cancel()
		if err == nil && r.Status == grpc_health_v1.HealthCheckResponse_SERVING {
			return nil
		}
		if status.Code(err) == codes.Unimplemented {
			return errors.New("standard gRPC health service is required")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (m *Manager) pollHealth(name string, inst *instance) {
	ticker := time.NewTicker(m.healthInterval)
	defer ticker.Stop()
	client := grpc_health_v1.NewHealthClient(inst.conn)
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(m.ctx, m.timeHealth)
			r, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
			cancel()
			if err == nil && r.Status != grpc_health_v1.HealthCheckResponse_SERVING {
				err = errors.New("health is not SERVING")
			}
			if err != nil {
				m.fail(name, fmt.Errorf("health check: %w", err))
				return
			}
		}
	}
}

func (m *Manager) watchHealth(name string, inst *instance) {
	stream, err := grpc_health_v1.NewHealthClient(inst.conn).Watch(m.ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		m.fail(name, fmt.Errorf("health watch: %w", err))
		return
	}
	for {
		r, err := stream.Recv()
		if err != nil {
			m.fail(name, fmt.Errorf("health watch: %w", err))
			return
		}
		if r.Status != grpc_health_v1.HealthCheckResponse_SERVING {
			m.fail(name, errors.New("health is not SERVING"))
			return
		}
	}
}
