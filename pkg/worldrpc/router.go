package worldrpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hazim-j/agent-wow/pkg/world"
)

// Session is the gameplay API required by Handler. *world.Session implements it.
// Methods must support concurrent callers. Canceling a Logout context should
// stop only that caller's wait, not the underlying gameplay session.
type Session interface {
	Snapshot() world.State
	Logout(context.Context) error
}

// routeMethod owns method dispatch, parameter validation, and gameplay error
// mapping. HTTP handling and JSON-RPC envelopes remain in server.go.
func routeMethod(ctx context.Context, session Session, method string, params json.RawMessage) (any, *rpcError) {
	if !emptyParams(params) {
		return nil, &rpcError{Code: -32602, Message: "Invalid params: this method takes no arguments"}
	}
	switch method {
	case "session.getState":
		return session.Snapshot(), nil
	case "session.logout":
		if err := session.Logout(ctx); err != nil {
			fault := &rpcError{Code: -32000, Message: err.Error()}
			var rejection *world.LogoutError
			if errors.As(err, &rejection) {
				fault.Data = struct {
					Reason uint32 `json:"reason"`
				}{rejection.Reason}
			}
			return nil, fault
		}
		return struct {
			Status world.Status `json:"status"`
		}{world.Closed}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "Method not found"}
	}
}

func emptyParams(params json.RawMessage) bool {
	if params == nil {
		return true
	}
	var value any
	if json.Unmarshal(params, &value) != nil {
		return false
	}
	switch v := value.(type) {
	case nil:
		return true
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}
