// Package opcode defines the wire identifiers used by this WoW 3.3.5a
// build-12340 client.
//
// CMSG names identify client-to-worldserver messages, SMSG names identify
// worldserver-to-client messages, and MSG names identify bidirectional messages.
// Auth names belong to the separate authserver protocol. Warden names identify
// commands and checks inside encrypted Warden payloads, not world packet headers.
//
// Constants are untyped so callers can use the protocol's byte, uint16, or
// uint32 wire fields without converting between an additional opcode type.
// World constants cover the complete AzerothCore Opcodes enum, including
// obsolete, UMSG, and TC9 entries. WorldName returns their upstream labels.
// A named opcode may still be unhandled by this client or disabled by the server.
package opcode
