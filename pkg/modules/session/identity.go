package modsession

// Identity identifies the character and realm selected for a gameplay session.
type Identity struct {
	CharacterGUID uint64
	CharacterName string
	RealmID       uint8
	RealmName     string
}
