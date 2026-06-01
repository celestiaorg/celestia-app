package rsema1d

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
)

// Config holds all configurable parameters for the codec.
//
// Both K and K+N must be powers of 2: the row Merkle tree has K+N leaves and
// the RLC tree has K leaves, and the tree requires a power-of-2 leaf count.
type Config struct {
	// Core parameters (required)
	K int // Number of original rows (power of 2)
	N int // Number of parity rows (K+N must be a power of 2)

	// Optional parameters with defaults
	WorkerCount int // Number of parallel workers (minimum 1)
}

// DefaultConfig returns a standard configuration
func DefaultConfig() *Config {
	return &Config{
		K:           32768, // 32768 original rows
		N:           32768, // 32768 parity rows
		WorkerCount: runtime.NumCPU(),
	}
}

// Validate checks configuration constraints
func (c *Config) Validate() error {
	if c.K <= 0 {
		return errors.New("k must be positive")
	}
	if c.N <= 0 {
		return errors.New("n must be positive")
	}
	if c.K&(c.K-1) != 0 {
		return fmt.Errorf("k must be a power of 2, got %d", c.K)
	}
	total := c.K + c.N
	if total&(total-1) != 0 {
		return fmt.Errorf("k+n must be a power of 2, got %d", total)
	}

	// Check K + N <= 65536 (GF(2^16) field size limit)
	if total > 65536 {
		return fmt.Errorf("k + n must be <= 65536, got %d", total)
	}

	// WorkerCount must be at least 1
	if c.WorkerCount < 1 {
		return errors.New("WorkerCount must be at least 1")
	}

	return nil
}

// TreeBufferSize returns the byte size of the Merkle-tree node storage an encode
// needs — the row tree over K+N leaves plus the RLC tree over K leaves. It is
// the minimum length for a [Coder.EncodeWithTree] buffer.
func (c *Config) TreeBufferSize() int {
	return merkle.TreeBufferSize(c.K+c.N) + merkle.TreeBufferSize(c.K)
}

// nextPowerOfTwo returns the smallest power of 2 >= n.
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	if n&(n-1) == 0 {
		return n
	}
	power := 1
	for power < n {
		power <<= 1
	}
	return power
}
