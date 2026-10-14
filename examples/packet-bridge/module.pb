
ä
google/protobuf/any.protogoogle.protobuf"6
Any
type_url (	RtypeUrl
value (RvalueBv
com.google.protobufBAnyProtoPZ,google.golang.org/protobuf/types/known/anypb¢GPBªGoogle.Protobuf.WellKnownTypesbproto3
¾
google/protobuf/empty.protogoogle.protobuf"
EmptyB}
com.google.protobufB
EmptyProtoPZ.google.golang.org/protobuf/types/known/emptypbø¢GPBªGoogle.Protobuf.WellKnownTypesbproto3
§
api/module/v1/session.protoagentwow.module.v1google/protobuf/any.protogoogle/protobuf/empty.proto"?
WorldPacket
opcode (Ropcode
payload (Rpayload"E
SendPacketRequest
opcode (Ropcode
payload (Rpayload"5
ClockResponse$
client_time_ms (RclientTimeMs"u
InvokeModuleRequest
module (	Rmodule
method (	Rmethod.
request (2.google.protobuf.AnyRrequest"D
InvokeModuleResponse,
result (2.google.protobuf.AnyRresult2€
SessionK

SendPacket%.agentwow.module.v1.SendPacketRequest.google.protobuf.EmptyE
GetClock.google.protobuf.Empty!.agentwow.module.v1.ClockResponsea
InvokeModule'.agentwow.module.v1.InvokeModuleRequest(.agentwow.module.v1.InvokeModuleResponseB5Z3github.com/agent-wow/agent-wow/pkg/modules/v1;modv1bproto3
ê
module.proto!agentwow.examples.packetbridge.v1api/module/v1/session.protogoogle/protobuf/empty.proto"V
Packet
sequence (Rsequence
opcode (Ropcode
payload (Rpayload"9
PollRequest
after (Rafter
limit (Rlimit"•
PollResponse
latest (Rlatest
oldest (Roldest
gap (RgapC
packets (2).agentwow.examples.packetbridge.v1.PacketRpackets2ƒ
PacketBridgeE
Send%.agentwow.module.v1.SendPacketRequest.google.protobuf.Emptyg
Poll..agentwow.examples.packetbridge.v1.PollRequest/.agentwow.examples.packetbridge.v1.PollResponseC
OnPacket.agentwow.module.v1.WorldPacket.google.protobuf.EmptyBDZBgithub.com/agent-wow/agent-wow/examples/packet-bridge/api;bridgev1bproto3