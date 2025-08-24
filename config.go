package rsema1d

import (
	"errors"
	"fmt"
	"runtime"
)

// Config holds all configurable parameters for the codec
type Config struct {
	// Core parameters (required)
	K       int // Number of original rows
	N       int // Number of parity rows
	RowSize int // Size of each row in bytes (multiple of 64)

	// Optional parameters with defaults
	WorkerCount int // Number of parallel workers (minimum 1)
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
		return errors.New("K must be positive")
	}
	if c.N <= 0 {
		return errors.New("N must be positive")
	}
	if c.RowSize <= 0 {
		return errors.New("RowSize must be positive")
	}

	// Check K is a power of 2 (ensures left subtree is perfect)
	if !isPowerOfTwo(c.K) {
		return fmt.Errorf("K must be a power of 2, got %d", c.K)
	}
	
	// Check K + N is a power of 2 (ensures total tree is perfect)
	if !isPowerOfTwo(c.K + c.N) {
		return fmt.Errorf("K + N must be a power of 2, got %d", c.K+c.N)
	}

	// Check K + N <= 65536 (GF(2^16) field size limit)
	if c.K+c.N > 65536 {
		return fmt.Errorf("K + N must be <= 65536, got %d", c.K+c.N)
	}

	// Check RowSize is multiple of 64 (Leopard constraint)
	if c.RowSize%64 != 0 {
		return fmt.Errorf("RowSize must be a multiple of 64, got %d", c.RowSize)
	}

	// Check RowSize is at least 64
	if c.RowSize < 64 {
		return fmt.Errorf("RowSize must be at least 64, got %d", c.RowSize)
	}

	// WorkerCount must be at least 1
	if c.WorkerCount < 1 {
		return errors.New("WorkerCount must be at least 1")
	}

	return nil
}

// isPowerOfTwo checks if n is a power of 2
func isPowerOfTwo(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}