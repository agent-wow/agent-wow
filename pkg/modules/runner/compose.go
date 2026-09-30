// Package modrunner starts and stops session-owned module services with Docker Compose.
package modrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hazim-j/agent-wow/pkg/modules/discovery"
)

// Launch describes one module's session-owned service resources.
type Launch struct {
	Manifest    moddisc.Manifest
	Project     string
	Override    string
	SocketDir   string
	Environment map[string]string
}

// Runner starts and cleans up module services. Implementations must honor cancellation,
// isolate cleanup to the launch, and preserve persistent volumes.
type Runner interface {
	Up(context.Context, Launch) error
	Down(context.Context, Launch) error
}

// ComposeRunner uses the Docker Compose CLI with a local Linux Docker Engine.
type ComposeRunner struct{}

type tailBuffer struct{ data []byte }

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.data = append(b.data, p...)
	if len(b.data) > 16<<10 {
		b.data = bytes.Clone(b.data[len(b.data)-(16<<10):])
	}
	return n, nil
}
func docker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.WaitDelay = 2 * time.Second
	var out tailBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out.data)))
	}
	return strings.TrimSpace(string(out.data)), nil
}

func (ComposeRunner) Up(ctx context.Context, l Launch) error {
	if err := writeOverride(l); err != nil {
		return err
	}
	if runtime.GOOS != "linux" {
		return errors.New("modules require Linux and a local Docker engine")
	}
	host := os.Getenv("DOCKER_HOST")
	if host == "" || os.Getenv("DOCKER_CONTEXT") != "" {
		var err error
		host, err = docker(ctx, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
		if err != nil {
			return err
		}
	}
	if !strings.HasPrefix(host, "unix://") {
		return errors.New("modules require a local Docker Unix socket; remote engines are unsupported")
	}
	args := composeArgs(l)
	_, err := docker(ctx, append(args, "up", "--detach", "--build", l.Manifest.Compose.Service)...)
	return err
}

func (ComposeRunner) Down(ctx context.Context, l Launch) error {
	_, err := docker(ctx, append(composeArgs(l), "down", "--timeout", "5")...)
	return err
}
func composeArgs(l Launch) []string {
	return []string{"compose", "--project-name", l.Project, "--project-directory", l.Manifest.Directory, "-f", filepath.Join(l.Manifest.Directory, l.Manifest.Compose.File), "-f", l.Override}
}

func writeOverride(l Launch) error {
	// Each container sees only its own directory. The private parent is never
	// mounted, so world-writable socket permissions do not expose other modules.
	env := make(map[string]string, len(l.Environment))
	for key, value := range l.Environment {
		env[key] = strings.ReplaceAll(value, "$", "$$")
	}
	model := map[string]any{"services": map[string]any{l.Manifest.Compose.Service: map[string]any{
		"environment": env,
		"restart":     "no",
		"volumes":     []any{map[string]any{"type": "bind", "source": strings.ReplaceAll(l.SocketDir, "$", "$$"), "target": "/run/agent-wow"}},
	}}}
	body, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(l.Override, body, 0600)
}
