//go:build !arm64 || purego

package merkle

const hasSHA2x2 = false

func blockSHA2x2(a, b *[8]uint32, pa, pb []byte) { panic("unreachable") }
