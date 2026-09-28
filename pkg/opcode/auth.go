package opcode

// Authserver commands use the same byte identifier for requests and responses.
const (
	AuthLogonChallenge     = 0x00
	AuthLogonProof         = 0x01
	AuthReconnectChallenge = 0x02
	AuthReconnectProof     = 0x03
	AuthRealmList          = 0x10
)
