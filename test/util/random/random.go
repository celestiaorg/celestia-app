package random

import (
	"math/rand"
	"sync"
	"time"
)

// Seeding a source is costly, so Bytes shares one; the lock makes it safe for parallel tests.
var (
	mu  sync.Mutex
	src = New()
)

// New creates a new random object with a random seed.
func New() *rand.Rand {
	seed := time.Now().UnixNano()
	return rand.New(rand.NewSource(seed))
}

// Bytes generates random bytes using math/rand.
func Bytes(n int) []byte {
	mu.Lock()
	defer mu.Unlock()
	return BytesR(src, n)
}

// BytesR generates a slice of n random bytes using the provided *rand.Rand instance.
func BytesR(r *rand.Rand, n int) []byte {
	bz := make([]byte, n)
	_, _ = r.Read(bz) // never returns an error
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

// Float64 generates a random float64 using stdlib math/rand.
func Float64() float64 {
	return rand.Float64()
}
