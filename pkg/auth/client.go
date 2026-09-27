// Package auth implements AzerothCore's WoW 3.3.5a (build 12340) login protocol.
package auth

import (
	"errors"
	"net"
	"strconv"
	"strings"
)

// Client authenticates accounts and checks sessions with one authserver.
// Each operation opens and closes its own connection.
type Client struct {
	address string
}

// NewClient creates a client for an authserver at host:port without connecting.
func NewClient(address string) (*Client, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" {
		return nil, errors.New("authserver address must be host:port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, errors.New("authserver port must be between 1 and 65535")
	}
	return &Client{address: address}, nil
}
