package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	bridgev1 "github.com/agent-wow/agent-wow/examples/packet-bridge/api"
	"github.com/agent-wow/agent-wow/pkg/modules/discovery"
	modv1 "github.com/agent-wow/agent-wow/pkg/modules/v1"
	"github.com/agent-wow/agent-wow/pkg/opcode"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeSession struct {
	sends int
	err   error
}

func (s *fakeSession) SendPacket(_ context.Context, _ *modv1.SendPacketRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	s.sends++
	return &emptypb.Empty{}, s.err
}
func (s *fakeSession) GetClock(context.Context, *emptypb.Empty, ...grpc.CallOption) (*modv1.ClockResponse, error) {
	return nil, errors.New("unused")
}
func (s *fakeSession) InvokeModule(context.Context, *modv1.InvokeModuleRequest, ...grpc.CallOption) (*modv1.InvokeModuleResponse, error) {
	return nil, errors.New("unused")
}

func TestSendDoesNotRetryUncertainWrite(t *testing.T) {
	want := errors.New("transport outcome unknown")
	session := &fakeSession{err: want}
	b := &bridge{session: session}
	_, err := b.Send(context.Background(), &modv1.SendPacketRequest{Opcode: 1})
	if !errors.Is(err, want) || session.sends != 1 {
		t.Fatalf("send calls=%d, error=%v", session.sends, err)
	}
}

func TestPollCursorAndPacketCopies(t *testing.T) {
	b := &bridge{}
	payload := []byte{1, 2, 3}
	_, err := b.OnPacket(context.Background(), &modv1.WorldPacket{Opcode: 42, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 9
	first, err := b.Poll(context.Background(), &bridgev1.PollRequest{})
	if err != nil || first.Latest != 1 || first.Oldest != 1 || first.Gap || len(first.Packets) != 1 || first.Packets[0].Opcode != 42 || !bytes.Equal(first.Packets[0].Payload, []byte{1, 2, 3}) {
		t.Fatalf("first poll: %v, %v", first, err)
	}
	first.Packets[0].Payload[0] = 8
	second, err := b.Poll(context.Background(), &bridgev1.PollRequest{})
	if err != nil || second.Packets[0].Payload[0] != 1 {
		t.Fatalf("buffer changed through response: %v, %v", second, err)
	}
	after, err := b.Poll(context.Background(), &bridgev1.PollRequest{After: 1})
	if err != nil || len(after.Packets) != 0 || after.Gap {
		t.Fatalf("cursor poll: %v, %v", after, err)
	}
}

func TestPollReportsOverflowAndPaginates(t *testing.T) {
	b := &bridge{}
	for i := 0; i < packetLimit+4; i++ {
		if _, err := b.OnPacket(context.Background(), &modv1.WorldPacket{Opcode: 42, Payload: []byte{byte(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := b.Poll(context.Background(), &bridgev1.PollRequest{After: 0, Limit: 2})
	if err != nil || !first.Gap || first.Latest != packetLimit+4 || first.Oldest != 5 || len(first.Packets) != 2 || first.Packets[0].Sequence != 5 || first.Packets[1].Sequence != 6 {
		t.Fatalf("overflow poll: %v, %v", first, err)
	}
	second, err := b.Poll(context.Background(), &bridgev1.PollRequest{After: 6, Limit: 2})
	if err != nil || second.Gap || len(second.Packets) != 2 || second.Packets[0].Sequence != 7 {
		t.Fatalf("next page: %v, %v", second, err)
	}
}

func TestOversizedPacketCannotHideGap(t *testing.T) {
	b := &bridge{}
	for _, payload := range [][]byte{{1}, make([]byte, byteLimit+1), {3}} {
		if _, err := b.OnPacket(context.Background(), &modv1.WorldPacket{Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	response, err := b.Poll(context.Background(), &bridgev1.PollRequest{After: 1})
	if err != nil || !response.Gap || response.Latest != 3 || response.Oldest != 3 || len(response.Packets) != 1 || response.Packets[0].Sequence != 3 {
		t.Fatalf("oversized packet poll: %v, %v", response, err)
	}
}

func TestByteBudgetEvictsOldest(t *testing.T) {
	b := &bridge{}
	for range 3 {
		if _, err := b.OnPacket(context.Background(), &modv1.WorldPacket{Payload: make([]byte, byteLimit/2)}); err != nil {
			t.Fatal(err)
		}
	}
	response, err := b.Poll(context.Background(), &bridgev1.PollRequest{})
	if err != nil || !response.Gap || response.Oldest != 2 || len(response.Packets) != 2 {
		t.Fatalf("byte budget poll: latest=%d oldest=%d count=%d gap=%v err=%v", response.Latest, response.Oldest, len(response.Packets), response.Gap, err)
	}
}

func TestManifestLoadsIntoAgentWow(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "packet-bridge")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"module.yaml", "compose.yaml", "module.pb"} {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := moddisc.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := registry.Lookup("packet-bridge")
	if !ok {
		t.Fatal("module missing from registry")
	}
	for _, alias := range []string{"send", "poll"} {
		if _, ok := definition.RPC(alias); !ok {
			t.Fatalf("missing %s RPC", alias)
		}
	}
	if _, ok := definition.Packet(opcode.SMSGUpdateObject); !ok {
		t.Fatal("missing object update subscription")
	}
	if _, ok := definition.Packet(opcode.SMSGQueryTimeResponse); !ok {
		t.Fatal("missing time query response subscription")
	}
}
