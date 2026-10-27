//go:build !arm64 || noasm || race

package sha256x2

// asmBuild reports that this build cannot take the two-lane path: it is false
// under -race, -tags noasm, and on non-arm64.
const asmBuild = false

const enabled = false

func block(a, b *[8]uint32, pa, pb []byte) { panic("unreachable") }
