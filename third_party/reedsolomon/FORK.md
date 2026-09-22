# Reed–Solomon source provenance

This directory contains the root package sources, tests, module files, README,
and MIT license from [klauspost/reedsolomon v1.14.2](https://github.com/klauspost/reedsolomon/tree/v1.14.2),
commit `af9e2b1b1bad1889954523347758996aafd9c805`.
The Go module checksum is `h1:SafJYwpBBQBI6amHUygcjxZjXeN2HpiENHQDwuPWCCQ=`.
Upstream CI, example programs, and the standalone benchmark program are omitted.
Generated platform kernels are retained without regeneration.

Local changes:

- `galois_leopard_arm64.s`: GF16 NEON multiply and two-way FFT/IFFT kernels.
- `galois_arm64.go`: feature-gated dispatch and race annotations for those kernels.
- `leopard.go`: initialize nibble tables on ASIMD and use a 16 KiB chunk floor
  when NEON assembly is enabled; other paths retain the 4 KiB floor.
- `galois_leopard_arm64_test.go`: reference equality, boundary, reconstruction,
  and production-shape coverage.

AMD64 AVX512/GFNI/AVX2 code and all other upstream files are unchanged.
The module path is preserved; celestia-app selects this directory with a relative
`go.mod` replacement. Go replacements are not inherited by downstream modules.
