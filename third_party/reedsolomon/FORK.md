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
- `galois_eor3_arm64.s`: EOR3 (FEAT_SHA3) variants of the two-way kernels,
  of `mulgf16` and of the 8-way accumulate, plus fused radix-4 FFT/IFFT
  kernels that take a nil table for a modulus (zero product) multiplier;
  selected by `options.useSHA3`.
- `galois_arm64.go`: feature-gated dispatch and race annotations; 4-way
  butterflies use the fused kernels, or 1 KiB byte blocks without SHA3.
  `xorSlices` folds several source rows into one destination pass
  (`xorSlicesNEON` in `galois_leopard_arm64.s`).
- `options.go`: `useSHA3` detected from cpuid, cleared by `WithNEON(false)`.
- `leopard.go`: initialize nibble tables on ASIMD and use a 16 KiB chunk floor
  when NEON assembly is enabled; other paths retain the 4 KiB floor. On NEON the
  encoder runs the FFT and IFFT radix-4 stages grouped by 64 rows
  (`fftDITGrouped`, `ifftDITEncoderGrouped`) and skips the zero rows of the
  IFFT top stage via `splitMulXor`. When `m == 4*dataShards` and m is a power
  of 4, the IFFT and FFT top stages are merged into scaling the IFFT output by
  four constants (`leopard_top.go`). On NEON the decoder runs the IFFT and
  the sparse FFT grouped the same way (`ifftDITDecoderGrouped`,
  `fftDITGrouped` with an `errorBitfield`) and computes the formal derivative
  row by row with `xorSlices` (`formalDerivative`).
- `leopard_split_generic.go`: `splitMulXor` and `xorSlices` for non-NEON
  builds.
- `galois_leopard_arm64_test.go`, `leopard_grouped_test.go`,
  `leopard_derivative_test.go`, `leopard_golden_test.go`,
  `leopard_golden_reconstruct_test.go`, `leopard_bench_test.go`: kernel
  reference equality (incl. EOR3, split, fused and multi-source xor kernels),
  grouped-vs-ungrouped and derivative equality, pinned parity and reconstruct
  SHA-256 digests, and the production-shape encode and reconstruct benchmarks.

AMD64 AVX512/GFNI/AVX2 code and all other upstream files are unchanged.
The module path is preserved; celestia-app selects this directory with a relative
`go.mod` replacement. Go replacements are not inherited by downstream modules.
