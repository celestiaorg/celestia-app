package rsema1d

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/celestiaorg/rsema1d/field"
)

// deriveCoefficients generates RLC coefficients via Fiat-Shamir (internal)
func deriveCoefficients(rowRoot [32]byte, config *Config) []field.GF128 {
	seed := sha256.Sum256(rowRoot[:])
	numSymbols := config.RowSize / 2 // Each GF16 symbol is 2 bytes
	coeffs := make([]field.GF128, numSymbols)

	for i := 0; i < numSymbols; i++ {
		h := sha256.New()
		h.Write(seed[:])
		binary.Write(h, binary.LittleEndian, uint32(i))
		coeffs[i] = field.HashToGF128(h.Sum(nil))
	}
	return coeffs
}

// computeRLC computes random linear combination for a row (internal)
func computeRLC(row []byte, coeffs []field.GF128, config *Config) field.GF128 {
	result := field.Zero()
	numChunks := len(row) / chunkSize

	for c := 0; c < numChunks; c++ {
		chunk := row[c*chunkSize : (c+1)*chunkSize]
		symbols := extractSymbols(chunk)
		for j, sym := range symbols {
			// result += symbol * coefficient
			symbolIndex := c*32 + j // Overall symbol index in the row
			product := field.Mul128(sym, coeffs[symbolIndex])
			result = field.Add128(result, product)
		}
	}
	return result
}

// extractSymbols extracts GF16 symbols from Leopard-formatted chunk (internal)
// Implements Appendix A.1 from spec
func extractSymbols(chunk []byte) []field.GF16 {
	if len(chunk) != chunkSize {
		panic("extractSymbols requires exactly 64-byte chunk")
	}

	symbols := make([]field.GF16, 32)
	for i := 0; i < 32; i++ {
		// Leopard format: bytes 0-31 are low bytes, 32-63 are high bytes
		symbols[i] = field.GF16(chunk[32+i])<<8 | field.GF16(chunk[i])
	}
	return symbols
}