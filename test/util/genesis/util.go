package genesis

import (
	"io"
	mrand "math/rand"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
)

func NewSeed(r *mrand.Rand) []byte {
	seed := make([]byte, ed25519.SeedSize)

	_, err := io.ReadFull(r, seed)
	if err != nil {
		panic(err) // this shouldn't happen
	}
	return seed
}

func GenerateEd25519(seed []byte) crypto.PrivKey {
	return ed25519.GenPrivKeyFromSecret(seed)
}
