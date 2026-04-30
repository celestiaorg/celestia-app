package rsema1d

import (
	"errors"
	"fmt"
	"runtime"
)

// Config holds all configurable parameters for the codec
type Config struct {
	// Core parameters (required)
	K       int // Number of original rows (can be arbitrary)
	N       int // Number of parity rows (can be arbitrary)
	RowSize int // Size of each row in bytes (multiple of 64)

	// Optional parameters with defaults
	WorkerCount int // Number of parallel workers (minimum 1)

	// Computed padding values (set during Validate)
	kPadded     int // Next power of 2 >= K
	totalPadded int // Next power of 2 >= (kPadded + N)
}

// DefaultConfig returns a standard configuration
func DefaultConfig() *Config {
	return &Config{
		K:           32768, // 32768 rows × 4096 bytes = 128 MB of original data
		N:           32768, // 32768 parity rows = 128 MB of parity data
		RowSize:     4096,
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
	// Check K + N <= 65536 (GF(2^16) field size limit)
	if c.K+c.N > 65536 {
		return fmt.Errorf("k + n must be <= 65536, got %d", c.K+c.N)
	}

	// RowSize must be at least 64 and a multiple of 64 (Leopard constraint).
	// It is also bound into the Fiat-Shamir transcript, so a zero/unspecified
	// value would produce coefficients that cannot match the prover's.
	if c.RowSize < 64 {
		return fmt.Errorf("RowSize must be at least 64, got %d", c.RowSize)
	}
	if c.RowSize%64 != 0 {
		return fmt.Errorf("RowSize must be a multiple of 64, got %d", c.RowSize)
	}

	// WorkerCount must be at least 1
	if c.WorkerCount < 1 {
		return errors.New("WorkerCount must be at least 1")
	}

	// Compute padding values for tree construction
	c.kPadded = nextPowerOfTwo(c.K)
	c.totalPadded = nextPowerOfTwo(c.kPadded + c.N)

	return nil
}

// nextPowerOfTwo returns the smallest power of 2 >= n
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	// If already power of 2, return it
	if n&(n-1) == 0 {
		return n
	}
	// Find next power of 2
	power := 1
	for power < n {
		power <<= 1
	}
	return power
}
