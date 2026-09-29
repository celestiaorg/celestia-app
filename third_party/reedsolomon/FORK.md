# Reed–Solomon source provenance

This directory contains the root package sources, tests, module files, README,
and MIT license from [klauspost/reedsolomon v1.14.2](https://github.com/klauspost/reedsolomon/tree/v1.14.2),
commit `af9e2b1b1bad1889954523347758996aafd9c805`.
The Go module checksum is `h1:SafJYwpBBQBI6amHUygcjxZjXeN2HpiENHQDwuPWCCQ=`.
Upstream CI, example programs, and the standalone benchmark program are omitted.
Generated platform kernels are retained without regeneration.

Local changes:

- `galois_leopard_arm64.s`: GF16 NEON multiply, two-way FFT/IFFT and split
  (`y = x; x ^= x*m`) kernels.
- `galois_eor3_arm64.s`: EOR3 (FEAT_SHA3) variants of the two-way kernels plus
  fused radix-4 FFT/IFFT kernels; selected by `options.useSHA3`.
- `galois_arm64.go`: feature-gated dispatch and race annotations; 4-way
  butterflies use the fused kernels, or 1 KiB byte blocks without SHA3.
- `options.go`: `useSHA3` detected from cpuid, cleared by `WithNEON(false)`.
- `leopard.go`: initialize nibble tables on ASIMD and use a 32 KiB chunk floor
  when NEON assembly is enabled; other paths retain the 4 KiB floor. On NEON the
  encoder runs the FFT and IFFT radix-4 stages grouped by 64 rows
  (`fftDITGrouped`, `ifftDITEncoderGrouped`) and skips the zero rows of the
  IFFT top stage via `splitMulXor`.
- `leopard_split_generic.go`: `splitMulXor` for non-NEON builds.
- `galois_leopard_arm64_test.go`, `leopard_grouped_test.go`,
  `leopard_golden_test.go`, `leopard_bench_test.go`: kernel reference equality
  (incl. EOR3, split and fused kernels), grouped-vs-ungrouped equality, pinned
  parity SHA-256 digests, and the production-shape benchmark.

AMD64 AVX512/GFNI/AVX2 code and all other upstream files are unchanged.
The module path is preserved; celestia-app selects this directory with a relative
`go.mod` replacement. Go replacements are not inherited by downstream modules.
