package worldrpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/agent-wow/agent-wow/pkg/modules/runtime"
	"github.com/agent-wow/agent-wow/pkg/world"
	"google.golang.org/grpc/status"
)

// Session is the gameplay API required by Handler. *world.Session implements it.
// Methods must support concurrent callers. Canceling a Logout context should
// stop only that caller's wait, not the underlying gameplay session.
type Session interface {
	InvokeJSON(context.Context, string, json.RawMessage) (json.RawMessage, error)
	Logout(context.Context) error
}

// routeMethod owns method dispatch, parameter validation, and gameplay error
// mapping. HTTP handling and JSON-RPC envelopes remain in server.go.
func routeMethod(ctx context.Context, session Session, method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "session.logout":
		if !emptyParams(params) {
			return nil, &rpcError{Code: -32602, Message: "Invalid params: this method takes no arguments"}
		}
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
		result, err := session.InvokeJSON(ctx, method, params)
		if err == nil {
			return result, nil
		}
		var call *modrt.CallError
		if errors.As(err, &call) {
			st := status.Convert(call.Err)
			return nil, &rpcError{Code: call.Code, Message: call.Error(), Data: map[string]any{
				"module": call.Module, "method": call.Method, "grpc_status": st.Code().String(),
				"grpc_message": st.Message(), "grpc_details": st.Proto().Details,
			}}
		}
		return nil, &rpcError{Code: -32000, Message: err.Error()}
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
