package modrt

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
)

func (m *Manager) BeginLogout() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preparing = true
	m.prepareDeadline = time.Now().Add(5 * time.Second)
}

func (m *Manager) PrepareLogout(ctx context.Context) error {
	m.mu.Lock()
	deadline := m.prepareDeadline
	m.mu.Unlock()
	if deadline.IsZero() {
		return errors.New("logout preparation was not begun")
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	// Reverse topological order ensures all callers have settled before their
	// dependencies prepare. Serial hooks share one bounded preparation budget.
	for i := len(m.order) - 1; i >= 0; i-- {
		name := m.order[i]
		inst := m.instances[name]
		if inst == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			m.fail(name, err)
			return err
		}
		if md, ok := inst.definition.BeforeLogout(); ok {
			if err := m.invoke(ctx, logoutCall, "before_logout", name, inst, md, &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
				m.fail(name, err)
				return m.Err()
			}
		}
		m.mu.Lock()
		inst.prepared = true
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.preparing = false
	m.prepared = true
	m.prepareDeadline = time.Time{}
	m.mu.Unlock()
	return nil
}

func (m *Manager) Resume() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preparing = false
	m.prepared = false
	m.prepareDeadline = time.Time{}
	for _, inst := range m.instances {
		inst.prepared = false
	}
}
