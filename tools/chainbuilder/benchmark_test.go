package main

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
)

func BenchmarkRun(b *testing.B) {
	cfg := BuilderConfig{
		NumBlocks:     100,
		BlockSize:     appconsts.DefaultMaxBytes,
		BlockInterval: time.Second,
	}

	dir := b.TempDir()

	for b.Loop() {
		if err := Run(context.Background(), cfg, dir); err != nil {
			b.Fatal(err)
		}
	}
}
