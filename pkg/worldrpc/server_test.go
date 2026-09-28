package worldrpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hazim-j/agent-wow/pkg/world"
)

type fakeSession struct{ logout func(context.Context) error }

func (s *fakeSession) Snapshot() world.State {
	return world.State{Status: world.InWorld, Character: world.Character{GUID: 9007199254740993, Name: "Mira"}, Realm: world.Realm{ID: 1, Name: "AzerothCore"}}
}
func (s *fakeSession) Logout(ctx context.Context) error {
	if s.logout != nil {
		return s.logout(ctx)
	}
	return nil
}
func call(handler http.Handler, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://127.0.0.1:8086/rpc", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func TestRPCMethodsAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		code, status int
	}{
		{"state", `{"jsonrpc":"2.0","id":9007199254740993,"method":"session.getState"}`, 0, 200},
		{"logout", `{"jsonrpc":"2.0","id":"bye","method":"session.logout","params":{}}`, 0, 200},
		{"empty array params", `{"jsonrpc":"2.0","id":null,"method":"session.getState","params":[]}`, 0, 200},
		{"notification", `{"jsonrpc":"2.0","method":"session.getState"}`, 0, 204},
		{"unknown", `{"jsonrpc":"2.0","id":1,"method":"movement.walk"}`, -32601, 200},
		{"args", `{"jsonrpc":"2.0","id":1,"method":"session.logout","params":{"force":true}}`, -32602, 200},
		{"scalar params", `{"jsonrpc":"2.0","id":1,"method":"session.logout","params":false}`, -32602, 200},
		{"batch", `[{"jsonrpc":"2.0","id":1,"method":"session.getState"}]`, -32600, 200},
		{"null request", `null`, -32600, 200},
		{"version", `{"jsonrpc":"1.0","id":1,"method":"session.getState"}`, -32600, 200},
		{"no method", `{"jsonrpc":"2.0","id":1}`, -32600, 200},
		{"bad ID", `{"jsonrpc":"2.0","id":true,"method":"session.getState"}`, -32600, 200},
		{"malformed", `{`, -32700, 200},
		{"trailing", `{} {}`, -32700, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(Handler(&fakeSession{}), tc.body)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.status == 204 {
				if w.Body.Len() != 0 {
					t.Fatal(w.Body.String())
				}
				return
			}
			var reply struct {
				ID     json.RawMessage `json:"id"`
				Error  *rpcError       `json:"error"`
				Result json.RawMessage `json:"result"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
			if tc.code != 0 {
				if reply.Error == nil || reply.Error.Code != tc.code {
					t.Fatal(w.Body.String())
				}
				return
			}
			if reply.Error != nil {
				t.Fatal(w.Body.String())
			}
			if tc.name == "state" && (!strings.Contains(string(reply.Result), `"guid":"9007199254740993"`) || string(reply.ID) != "9007199254740993") {
				t.Fatal("precision lost", w.Body.String())
			}
		})
	}
}

func TestRPCLogoutRejectionAndNotification(t *testing.T) {
	count := 0
	h := Handler(&fakeSession{logout: func(context.Context) error { count++; return &world.LogoutError{Reason: 1} }})
	w := call(h, `{"jsonrpc":"2.0","id":4,"method":"session.logout"}`)
	if !strings.Contains(w.Body.String(), `"code":-32000`) || !strings.Contains(w.Body.String(), `"reason":1`) {
		t.Fatal(w.Body.String())
	}
	w = call(h, `{"jsonrpc":"2.0","method":"session.logout"}`)
	if w.Code != 204 || w.Body.Len() != 0 || count != 2 {
		t.Fatal(w.Code, w.Body.String(), count)
	}
}

func TestLocalHTTPRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, host, contentType, origin, fetch string
		status                                               int
	}{
		{name: "GET", method: "GET", status: 405},
		{name: "OPTIONS", method: "OPTIONS", status: 405},
		{name: "wrong path", path: "/", status: 404},
		{name: "browser", origin: "https://example.com", status: 403},
		{name: "fetch", fetch: "same-origin", status: 403},
		{name: "rebound host", host: "example.com", status: 403},
		{name: "form", contentType: "text/plain", status: 415},
		{name: "missing content type", contentType: "-", status: 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method, path := tc.method, tc.path
			if method == "" {
				method = "POST"
			}
			if path == "" {
				path = "/rpc"
			}
			r := httptest.NewRequest(method, "http://127.0.0.1:8086"+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session.getState"}`))
			r.Header.Set("Content-Type", "application/json")
			if tc.host != "" {
				r.Host = tc.host
			}
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.fetch != "" {
				r.Header.Set("Sec-Fetch-Site", tc.fetch)
			}
			w := httptest.NewRecorder()
			Handler(&fakeSession{}).ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal(w.Code, w.Header())
			}
		})
	}
	if w := call(Handler(&fakeSession{}), strings.Repeat(" ", 65537)); w.Code != 413 {
		t.Fatal(w.Code)
	}
}

func TestLoopbackListener(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8086", ":0", "[::]:0", "192.168.1.1:0", "example.com:0", "localhost:65536", "127.0.0.1:-1", "127.0.0.1:http", "127.0.0.1"} {
		if listener, err := Listen(context.Background(), address); err == nil {
			listener.Close()
			t.Errorf("accepted %s", address)
		}
	}
	for _, address := range []string{"127.0.0.1:0", "localhost:0", "[::1]:0"} {
		listener, err := Listen(context.Background(), address)
		if err != nil {
			t.Fatal(err)
		}
		if second, err := Listen(context.Background(), listener.Addr().String()); err == nil {
			second.Close()
			t.Fatal("accepted occupied address")
		}
		listener.Close()
	}
}
