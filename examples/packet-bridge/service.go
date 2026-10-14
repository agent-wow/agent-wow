package main

import (
	"bytes"
	"context"
	"sync"

	bridgev1 "github.com/agent-wow/agent-wow/examples/packet-bridge/api"
	modv1 "github.com/agent-wow/agent-wow/pkg/modules/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	packetLimit = 256
	byteLimit   = 2 << 20
	defaultPoll = 64
)

// bridge retains a bounded, ordered view of subscribed world packets. The
// agent can detect evictions through PollResponse.gap and resync its state.
type bridge struct {
	bridgev1.UnimplementedPacketBridgeServer
	session modv1.SessionClient

	mu     sync.Mutex
	latest uint64
	bytes  int
	buffer []*bridgev1.Packet
}

func (b *bridge) Send(ctx context.Context, request *modv1.SendPacketRequest) (*emptypb.Empty, error) {
	// The session validates the opcode and sends it once. Retrying on an
	// uncertain transport outcome could duplicate an in-game action.
	return b.session.SendPacket(ctx, request)
}

func (b *bridge) OnPacket(_ context.Context, packet *modv1.WorldPacket) (*emptypb.Empty, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.latest++
	if len(packet.GetPayload()) > byteLimit {
		// Clear earlier packets as well so an omitted sequence always shows up
		// as a leading gap, even after subsequent packets arrive.
		b.buffer = nil
		b.bytes = 0
		return &emptypb.Empty{}, nil
	}
	entry := &bridgev1.Packet{
		Sequence: b.latest,
		Opcode:   packet.GetOpcode(),
		Payload:  bytes.Clone(packet.GetPayload()),
	}
	b.buffer = append(b.buffer, entry)
	b.bytes += len(entry.Payload)
	for len(b.buffer) > packetLimit || b.bytes > byteLimit {
		b.bytes -= len(b.buffer[0].Payload)
		b.buffer[0] = nil
		b.buffer = b.buffer[1:]
	}
	return &emptypb.Empty{}, nil
}

func (b *bridge) Poll(_ context.Context, request *bridgev1.PollRequest) (*bridgev1.PollResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	limit := request.GetLimit()
	if limit == 0 {
		limit = defaultPoll
	}
	if limit > packetLimit {
		limit = packetLimit
	}
	oldest := b.latest + 1
	if len(b.buffer) != 0 {
		oldest = b.buffer[0].Sequence
	}
	response := &bridgev1.PollResponse{
		Latest: b.latest,
		Oldest: oldest,
		Gap:    request.GetAfter() < oldest-1,
	}
	for _, packet := range b.buffer {
		if packet.Sequence <= request.GetAfter() {
			continue
		}
		response.Packets = append(response.Packets, &bridgev1.Packet{
			Sequence: packet.Sequence,
			Opcode:   packet.Opcode,
			Payload:  bytes.Clone(packet.Payload),
		})
		if uint32(len(response.Packets)) == limit {
			break
		}
	}
	return response, nil
}
