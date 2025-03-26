package random

import (
	"math/rand"
	"time"
)

// New creates a new random object with a random seed.
func New() *rand.Rand {
	seed := time.Now().UnixNano()
	return rand.New(rand.NewSource(seed))
}

// Bytes generates random bytes using math/rand.
func Bytes(n int) []byte {
	return BytesR(New(), n)
}

// BytesR generates a slice of n random bytes using the provided *rand.Rand instance.
func BytesR(r *rand.Rand, n int) []byte {
	bz := make([]byte, n)
	for i := range bz {
		bz[i] = byte(r.Intn(256)) // Random byte (0-255)
	}
	return bz
}

// Str generates a random string of the given size.
func Str(n int) string {
	return StrR(New(), n)
}

// StrR generates a random string of length n using the provided *rand.Rand instance.
func StrR(r *rand.Rand, n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	bz := make([]byte, n)
	for i := range bz {
		bz[i] = letters[r.Intn(len(letters))]
	}
	return string(bz)
}
