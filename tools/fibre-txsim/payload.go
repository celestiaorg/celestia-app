package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math/rand/v2"
)

// payloadGenerator generates synthetic data, not keys or secrets.
// It belongs to one upload worker and is not concurrency-safe.
type payloadGenerator struct {
	random   *rand.ChaCha8
	identity [32]byte
	sequence uint64
}

func newPayloadGenerator(identity string) *payloadGenerator {
	seed := sha256.Sum256([]byte(identity))
	return &payloadGenerator{random: rand.NewChaCha8(seed), identity: seed}
}

func (g *payloadGenerator) fill(ctx context.Context, data []byte) error {
	const chunkSize = 1 << 20
	for offset := 0; offset < len(data); offset += chunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, _ = g.random.Read(data[offset:min(offset+chunkSize, len(data))])
	}
	// Large payloads have a distinct identity even if the random stream repeats.
	if len(data) >= len(g.identity)+8 {
		copy(data, g.identity[:])
		binary.LittleEndian.PutUint64(data[len(g.identity):], g.sequence)
	}
	g.sequence++
	return ctx.Err()
}
