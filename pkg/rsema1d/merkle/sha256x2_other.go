//go:build !arm64 || purego

package merkle

// sha2x2Build reports that this build includes the two-lane SHA-256 kernel: it is
// false here (non-arm64, or -tags purego).
const sha2x2Build = false

const hasSHA2x2 = false

func blockSHA2x2(a, b *[8]uint32, pa, pb []byte) { panic("unreachable") }
