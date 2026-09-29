//go:build arm64 && !purego

package merkle

import "golang.org/x/sys/cpu"

var hasSHA2x2 = cpu.ARM64.HasSHA2

//go:noescape
func blockSHA2x2(a, b *[8]uint32, pa, pb []byte)
