package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"errors"
	"runtime"
	"testing"
)

func TestPayloadGenerator(t *testing.T) {
	seen := make(map[string]bool)
	for _, identity := range []string{"host-a/1/100/0", "host-a/1/100/1", "host-b/1/100/0", "host-a/1/101/0"} {
		g := newPayloadGenerator(identity)
		for range 3 {
			data := make([]byte, 1<<20+17)
			if err := g.fill(context.Background(), data); err != nil {
				t.Fatal(err)
			}
			prefix := string(data[:40])
			if seen[prefix] {
				t.Fatal("duplicate payload identity")
			}
			seen[prefix] = true
			var compressed bytes.Buffer
			writer := gzip.NewWriter(&compressed)
			if _, err := writer.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if compressed.Len() < len(data)*99/100 {
				t.Fatal("benchmark payload is compressible")
			}
		}
	}
}

func TestPayloadGeneratorSmallAndCancelled(t *testing.T) {
	g := newPayloadGenerator("worker")
	for size := range 41 {
		if err := g.fill(context.Background(), make([]byte, size)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := make([]byte, 1024)
	if err := g.fill(ctx, data); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
	if !bytes.Equal(data, make([]byte, len(data))) {
		t.Fatal("cancelled generator wrote payload")
	}
}

// Cancel at the next chunk boundary without depending on goroutine timing.
type cancelAfterChunk struct {
	context.Context
	checks int
}

func (c *cancelAfterChunk) Err() error {
	c.checks++
	if c.checks > 1 {
		return context.Canceled
	}
	return nil
}

func TestPayloadGeneratorCancellationBetweenChunks(t *testing.T) {
	data := make([]byte, 2<<20)
	ctx := &cancelAfterChunk{Context: context.Background()}
	if err := newPayloadGenerator("worker").fill(ctx, data); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
	if bytes.Equal(data[:1<<20], make([]byte, 1<<20)) {
		t.Fatal("first chunk was not generated")
	}
	if !bytes.Equal(data[1<<20:], make([]byte, 1<<20)) {
		t.Fatal("generator continued after cancellation")
	}
}

func BenchmarkPayload(b *testing.B) {
	for _, name := range []string{"ChaCha8", "CryptoRand"} {
		b.Run(name, func(b *testing.B) {
			data := make([]byte, 16<<20)
			g := newPayloadGenerator("benchmark")
			ctx := context.Background()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for b.Loop() {
				if name == "ChaCha8" {
					if err := g.fill(ctx, data); err != nil {
						b.Fatal(err)
					}
				} else if _, err := rand.Read(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPayloadParallel(b *testing.B) {
	previous := runtime.GOMAXPROCS(8)
	defer runtime.GOMAXPROCS(previous)
	for _, name := range []string{"ChaCha8", "CryptoRand"} {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(16 << 20)
			b.RunParallel(func(pb *testing.PB) {
				data := make([]byte, 16<<20)
				g := newPayloadGenerator("benchmark")
				for pb.Next() {
					if name == "ChaCha8" {
						if err := g.fill(context.Background(), data); err != nil {
							b.Error(err)
							return
						}
					} else if _, err := rand.Read(data); err != nil {
						b.Error(err)
						return
					}
				}
			})
		})
	}
}
