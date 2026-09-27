package auth

import (
	"crypto/rand"
	"crypto/sha1" // Required by the WoW SRP6 protocol.
	"errors"
	"math/big"
	"slices"
)

// WoW uses a fixed 256-bit SRP6 group and little-endian integers.
var modulus = func() *big.Int {
	n, ok := new(big.Int).SetString("894B645E89E1535BBDAD5B8B290650530801B18EBFBF5E8FAB3C82872A3E9BB7", 16)
	if !ok {
		panic("invalid SRP6 modulus")
	}
	return n
}()

type challenge struct {
	public [32]byte
	salt   [32]byte
}

type proof struct {
	public [32]byte
	client [20]byte
	server [20]byte
	key    [40]byte
}

func newProof(username, password string, c challenge) (proof, error) {
	// Draw a fresh, nonzero private exponent for every login.
	a, err := rand.Int(rand.Reader, new(big.Int).Sub(modulus, big.NewInt(1)))
	if err != nil {
		return proof{}, err
	}
	a.Add(a, big.NewInt(1))
	return calculateProof(username, password, c, a)
}

func calculateProof(username, password string, c challenge, a *big.Int) (proof, error) {
	var p proof
	b := fromLittleEndian(c.public[:])
	if b.Sign() == 0 || b.Cmp(modulus) >= 0 {
		return p, errors.New("authserver sent an invalid SRP6 public key")
	}
	g := big.NewInt(7)
	p.public = toLittleEndian(new(big.Int).Exp(g, a, modulus))
	uHash := digest(p.public[:], c.public[:])
	u := fromLittleEndian(uHash[:])
	if u.Sign() == 0 {
		return proof{}, errors.New("invalid SRP6 scrambling parameter")
	}
	identity := digest([]byte(username + ":" + password))
	xHash := digest(c.salt[:], identity[:])
	x := fromLittleEndian(xHash[:])
	v := new(big.Int).Exp(g, x, modulus)
	// S = (B - 3*g^x)^(a + u*x) mod N.
	base := new(big.Int).Sub(b, new(big.Int).Mul(big.NewInt(3), v))
	base.Mod(base, modulus)
	exponent := new(big.Int).Add(a, new(big.Int).Mul(u, x))
	shared := new(big.Int).Exp(base, exponent, modulus)
	if shared.Sign() == 0 {
		return proof{}, errors.New("invalid SRP6 shared secret")
	}
	p.key = sessionKey(toLittleEndian(shared))
	nBytes := toLittleEndian(modulus)
	groupHash := digest(nBytes[:])
	gHash := digest([]byte{7})
	for i := range groupHash {
		groupHash[i] ^= gHash[i]
	}
	usernameHash := digest([]byte(username))
	p.client = digest(groupHash[:], usernameHash[:], c.salt[:], p.public[:], c.public[:], p.key[:])
	p.server = digest(p.public[:], p.client[:], p.key[:])
	return p, nil
}

func sessionKey(shared [32]byte) [40]byte {
	// AzerothCore strips leading zero bytes, rounding up to an even offset,
	// before hashing the even and odd bytes separately and interleaving them.
	start := 0
	for start < len(shared) && shared[start] == 0 {
		start++
	}
	start += start % 2
	var even, odd []byte
	for i := start; i < len(shared); i += 2 {
		even = append(even, shared[i])
		odd = append(odd, shared[i+1])
	}
	evenHash, oddHash := digest(even), digest(odd)
	var key [40]byte
	for i := range evenHash {
		key[2*i], key[2*i+1] = evenHash[i], oddHash[i]
	}
	return key
}

func digest(parts ...[]byte) [20]byte {
	h := sha1.New()
	for _, part := range parts {
		_, _ = h.Write(part)
	}
	var result [20]byte
	copy(result[:], h.Sum(nil))
	return result
}

func fromLittleEndian(data []byte) *big.Int {
	reversed := slices.Clone(data)
	slices.Reverse(reversed)
	return new(big.Int).SetBytes(reversed)
}

func toLittleEndian(n *big.Int) [32]byte {
	var result [32]byte
	n.FillBytes(result[:])
	slices.Reverse(result[:])
	return result
}
