package main

import (
	"encoding/hex"
	"fmt"
	"log"

	"github.com/celestiaorg/celestia-app/v8/pkg/rsema1d"
)

func main() {
	// Test Case 1: K=4, N=4 (both power of 2, no padding needed)
	fmt.Println("### Test Vector 1: K=4, N=4, rowSize=64")
	fmt.Println("Power of 2 case - no padding needed")
	fmt.Println()
	config1 := &rsema1d.Config{
		K:           4,
		N:           4,
		RowSize:     64,
		WorkerCount: 1,
	}

	// Create simple test data - all zeros except last byte
	data1 := make([][]byte, 4)
	for i := 0; i < 4; i++ {
		data1[i] = make([]byte, 64)
		data1[i][63] = byte(i + 1) // Last byte: 0x01, 0x02, 0x03, 0x04
	}

	// Print input
	fmt.Println("Input data (4 rows × 64 bytes):")
	for i := 0; i < 4; i++ {
		fmt.Printf("Row %d: 0x%s...%02x\n", i,
			"00000000", data1[i][63])
	}

	// Encode and get commitment
	_, commitment1, _, err := rsema1d.Encode(data1, config1)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\nCommitment: 0x%s\n", hex.EncodeToString(commitment1[:]))
	fmt.Println()

	// Test Case 2: K=3, N=9 (1:3 ratio, arbitrary K, multi-chunk)
	fmt.Println("### Test Vector 2: K=3, N=9, rowSize=256")
	fmt.Println()

	config2 := &rsema1d.Config{
		K:           3,
		N:           9,
		RowSize:     256,
		WorkerCount: 1,
	}

	// Create test data - only 3 rows
	data2 := make([][]byte, 3)
	for i := 0; i < 3; i++ {
		data2[i] = make([]byte, 256)
		data2[i][255] = byte(i + 1) // Last byte: 0x01, 0x02, 0x03
	}

	// Print input
	fmt.Println("Input data (3 rows × 256 bytes):")
	for i := 0; i < 3; i++ {
		fmt.Printf("Row %d: 0x%s...%02x\n", i,
			"00000000", data2[i][255])
	}

	// Encode and get commitment
	_, commitment2, _, err := rsema1d.Encode(data2, config2)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\nCommitment: 0x%s\n", hex.EncodeToString(commitment2[:]))
}
