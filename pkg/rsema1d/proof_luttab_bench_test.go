package rsema1d

import (
	"crypto/sha256"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

// benchLogTabRowSize matches fibre's worst-case per-row size at K=4096 for a
// 128 MiB shard — 32768 bytes. This is the dominant size hit by the UploadShard
// verify path.
const benchLogTabRowSize = 32768

// makeLogTabBenchCase encodes a small K so we can feed real proofs through
// VerifyRow[s]WithContext. The row payload is still the production-size 32 KiB.
func makeLogTabBenchCase(tb testing.TB, nProofs int) (
	*ExtendedData, Commitment, []*RowProof, *Config, []field.GF128,
) {
	tb.Helper()
	cfg := &Config{K: 64, N: 64, RowSize: benchLogTabRowSize, WorkerCount: 1}
	data := make([][]byte, cfg.K)
	r := rand.New(rand.NewPCG(42, 42))
	for i := range data {
		data[i] = make([]byte, cfg.RowSize)
		for j := range data[i] {
			data[i][j] = byte(r.IntN(256))
		}
	}
	ed, commitment, rlcOrig, err := Encode(data, cfg)
	if err != nil {
		tb.Fatalf("Encode: %v", err)
	}
	proofs := make([]*RowProof, nProofs)
	for i := range proofs {
		idx := (i * 3) % (cfg.K + cfg.N)
		p, err := ed.GenerateRowProof(idx)
		if err != nil {
			tb.Fatalf("GenerateRowProof(%d): %v", idx, err)
		}
		proofs[i] = p
	}
	return ed, commitment, proofs, cfg, rlcOrig
}

// BenchmarkVerifyRowWithContext_LogTab exercises the single-row scalar verify
// path. The context is recreated each iteration so the first call pays the
// LogTab precompute. This measures the end-to-end per-row cost that the
// fallback inside VerifyRowsWithContext will see for the very first row.
func BenchmarkVerifyRowWithContext_LogTab(b *testing.B) {
	_, commitment, proofs, cfg, rlcOrig := makeLogTabBenchCase(b, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, _, err := CreateVerificationContext(rlcOrig, cfg)
		if err != nil {
			b.Fatalf("CreateVerificationContext: %v", err)
		}
		if err := VerifyRowWithContext(proofs[0], commitment, ctx); err != nil {
			b.Fatalf("VerifyRowWithContext: %v", err)
		}
	}
}

// BenchmarkVerifyRowWithContext_LogTab_Warm reuses the same context so each
// iteration pays only the hot-path computeRLCLogTab cost (no LogTab build,
// no deriveCoefficients). This is what subsequent scalar verifies in a
// shared context look like after the first call.
func BenchmarkVerifyRowWithContext_LogTab_Warm(b *testing.B) {
	_, commitment, proofs, cfg, rlcOrig := makeLogTabBenchCase(b, 1)
	ctx, _, err := CreateVerificationContext(rlcOrig, cfg)
	if err != nil {
		b.Fatalf("CreateVerificationContext: %v", err)
	}
	// Warm the coeffs+coeffLog caches outside the timed loop.
	if err := VerifyRowWithContext(proofs[0], commitment, ctx); err != nil {
		b.Fatalf("warmup VerifyRowWithContext: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := VerifyRowWithContext(proofs[0], commitment, ctx); err != nil {
			b.Fatalf("VerifyRowWithContext: %v", err)
		}
	}
}

// BenchmarkVerifyRowsWithContext_SmallBatch_LogTab measures the K<8 fallback
// regime where VerifyRowsWithContext loops over VerifyRowWithContext. This is
// the path the LogTab composition accelerates; K=16 and K=64 runs use the
// SIMD vectorized kernel and are included here as controls that should
// remain unchanged.
func BenchmarkVerifyRowsWithContext_SmallBatch_LogTab(b *testing.B) {
	for _, K := range []int{1, 4, 7, 8, 16, 64} {
		b.Run(fmt.Sprintf("K=%d", K), func(b *testing.B) {
			_, commitment, proofs, cfg, rlcOrig := makeLogTabBenchCase(b, K)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ctx, _, err := CreateVerificationContext(rlcOrig, cfg)
				if err != nil {
					b.Fatalf("CreateVerificationContext: %v", err)
				}
				if err := VerifyRowsWithContext(proofs, commitment, ctx); err != nil {
					b.Fatalf("VerifyRowsWithContext: %v", err)
				}
			}
		})
	}
}

// BenchmarkVerifyRowWithContext_ParallelLogTab mirrors the production load of
// a validator running several concurrent UploadShard verifications.
func BenchmarkVerifyRowWithContext_ParallelLogTab(b *testing.B) {
	_, commitment, proofs, cfg, rlcOrig := makeLogTabBenchCase(b, 1)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx, _, err := CreateVerificationContext(rlcOrig, cfg)
			if err != nil {
				b.Fatalf("CreateVerificationContext: %v", err)
			}
			if err := VerifyRowWithContext(proofs[0], commitment, ctx); err != nil {
				b.Fatalf("VerifyRowWithContext: %v", err)
			}
		}
	})
}

// BenchmarkComputeRLC_PreLogTabScalar measures the pre-LogTab scalar path
// (computeRLC) on a production-size row. This is the apples-to-apples
// baseline for BenchmarkComputeRLC_LogTab inside this same binary, letting
// us compute the speedup without needing to cross-build a different commit.
func BenchmarkComputeRLC_PreLogTabScalar(b *testing.B) {
	row, coeffs := makePreLogTabRow(b)
	b.SetBytes(int64(len(row)))
	b.ReportAllocs()
	b.ResetTimer()
	var sink field.GF128
	for i := 0; i < b.N; i++ {
		sink = computeRLC(row, coeffs)
	}
	_ = sink
}

// BenchmarkComputeRLC_LogTabHot measures the hot-path computeRLCLogTab (with
// the coeffLog precomputed). This isolates the per-row cost after the
// sync.Once build cost is paid.
func BenchmarkComputeRLC_LogTabHot(b *testing.B) {
	row, coeffs := makePreLogTabRow(b)
	cl := buildRLCCoeffLog(coeffs)
	b.SetBytes(int64(len(row)))
	b.ReportAllocs()
	b.ResetTimer()
	var sink field.GF128
	for i := 0; i < b.N; i++ {
		sink = computeRLCLogTab(row, cl)
	}
	_ = sink
}

// BenchmarkBuildRLCCoeffLog isolates the per-context LogTab precompute that
// the scalar verify path pays on first call.
func BenchmarkBuildRLCCoeffLog(b *testing.B) {
	_, coeffs := makePreLogTabRow(b)
	b.ReportAllocs()
	b.ResetTimer()
	var sink *rlcCoeffLog
	for i := 0; i < b.N; i++ {
		sink = buildRLCCoeffLog(coeffs)
	}
	_ = sink
}

func makePreLogTabRow(tb testing.TB) ([]byte, []field.GF128) {
	tb.Helper()
	row := make([]byte, benchLogTabRowSize)
	r := rand.New(rand.NewPCG(99, 7))
	for i := range row {
		row[i] = byte(r.IntN(256))
	}
	rowRoot := sha256.Sum256([]byte("bench-prelogtab"))
	coeffs := deriveCoefficients(rowRoot, len(row))
	return row, coeffs
}

// BenchmarkVerifyRowWithContext_Scalar replicates the pre-LogTab scalar path
// by constructing a fresh VerificationContext and calling deriveCoefficients
// + computeRLC (the functions used before this change). This isolates the
// "fresh context + single row" cost against BenchmarkVerifyRowWithContext_LogTab
// in the same binary.
func BenchmarkVerifyRowWithContext_Scalar(b *testing.B) {
	_, _, proofs, cfg, rlcOrig := makeLogTabBenchCase(b, 1)
	// Pre-extract the row and simulate the work that VerifyRowWithContext does.
	// Each iter: fresh context -> rebuild coeffs -> computeRLC -> compare.
	proof := proofs[0]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulated: rebuild context equivalent (deriveCoefficients), then
		// scalar RLC. We do not re-run Merkle here because Merkle work is a
		// small constant factor both before and after this change; isolating
		// the RLC-related delta is the point.
		var rowRoot [32]byte
		copy(rowRoot[:], proof.Row[:32]) // stand-in; consistent across iters
		coeffs := deriveCoefficients(rowRoot, len(proof.Row))
		_ = cfg
		_ = rlcOrig
		sink := computeRLC(proof.Row, coeffs)
		runtime_KeepAlive(sink)
	}
}

// BenchmarkVerifyRowWithContext_Scalar_Warm: cached coeffs path with scalar
// computeRLC — what the old steady-state VerifyRowWithContext looked like
// once coeffs were cached in the context.
func BenchmarkVerifyRowWithContext_Scalar_Warm(b *testing.B) {
	_, _, proofs, _, _ := makeLogTabBenchCase(b, 1)
	proof := proofs[0]
	var rowRoot [32]byte
	copy(rowRoot[:], proof.Row[:32])
	coeffs := deriveCoefficients(rowRoot, len(proof.Row))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink := computeRLC(proof.Row, coeffs)
		runtime_KeepAlive(sink)
	}
}

// runtime_KeepAlive prevents dead-code elimination of bench outputs without
// pulling in the full runtime package import just for this bench file.
func runtime_KeepAlive(x field.GF128) {
	// Force x onto the stack for the compiler.
	if x[0] == 0xDEAD && x[1] == 0xBEEF {
		panic("unreachable sentinel")
	}
}

// TestComputeRLCLogTabMatchesScalar locks in bit-for-bit equivalence with the
// reference computeRLC on a realistic row. This is the single test that
// guarantees the LUT bootstrap in commitment_luttab.go is wired up correctly
// and that the hot path does not diverge from klauspost's GF16Mul.
func TestComputeRLCLogTabMatchesScalar(t *testing.T) {
	row := make([]byte, benchLogTabRowSize)
	r := rand.New(rand.NewPCG(123, 456))
	for i := range row {
		row[i] = byte(r.IntN(256))
	}
	rowRoot := sha256.Sum256([]byte("lutTab-equiv"))
	coeffs := deriveCoefficients(rowRoot, len(row))

	want := computeRLC(row, coeffs)
	got := computeRLCLogTab(row, buildRLCCoeffLog(coeffs))

	if !field.Equal128(want, got) {
		t.Fatalf("LogTab diverges from scalar computeRLC\n want=%v\n got =%v", want, got)
	}
}
