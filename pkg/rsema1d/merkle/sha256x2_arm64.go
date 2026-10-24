//go:build arm64 && !purego

package merkle

import "golang.org/x/sys/cpu"

// sha2x2Build reports that this build includes the two-lane SHA-256 kernel. It is
// false under -tags purego and on non-arm64 (see sha256x2_other.go), so the
// RSEMA1D_REQUIRE_ASM check can tell a build-excluded kernel apart from a CPU
// that merely lacks the SHA2 instructions.
const sha2x2Build = true

// hasSHA2x2 reports whether blockSHA2x2 will run at runtime: the kernel is in
// this build and the CPU has the SHA2 instructions it uses.
var hasSHA2x2 = cpu.ARM64.HasSHA2

//go:noescape
func blockSHA2x2(a, b *[8]uint32, pa, pb []byte)
