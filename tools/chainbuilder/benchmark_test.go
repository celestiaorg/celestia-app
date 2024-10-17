package main

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
)

func BenchmarkRun(b *testing.B) {
	cfg := BuilderConfig{
		NumBlocks:     100,
		BlockSize:     appconsts.DefaultMaxBytes,
		BlockInterval: time.Second,
	}

	dir := b.TempDir()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Run(context.Background(), cfg, dir); err != nil {
			b.Fatal(err)
		}
	}
}
