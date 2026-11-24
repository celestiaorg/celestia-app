package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v12/test/util/fuzzutil"
	"github.com/stretchr/testify/require"
)

// FuzzFastRecheckDifferential drives the equivalence test's full and fast apps
// from fuzz input instead of a seed: every choice of txs, blocks, state changes
// and recheck order reads the input. Each input runs one to three rounds, each
// checked as in TestFastRecheckEquivalence.
//
// The app pair is kept across inputs and rebuilt every eqFuzzInputs inputs, so a
// failure can depend on the inputs since the last rebuild. They are saved to
// testdata/fast-recheck-history, which TestFastRecheckDifferentialHistories replays.
func FuzzFastRecheckDifferential(f *testing.F) {
	for _, seed := range eqFuzzSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		runEqFuzz(t, data)
	})
}

// TestFastRecheckDifferentialSeeds runs the seeds on one app pair and requires
// that they reach the fast path.
func TestFastRecheckDifferentialSeeds(t *testing.T) {
	hits := 0
	for _, seed := range eqFuzzSeeds() {
		hits += runEqFuzz(t, seed)
	}
	require.Positive(t, hits, "the seeds never reach the fast path")
}

const eqFuzzInputs = 16

var (
	eqFuzzMu      sync.Mutex
	eqFuzzEnv     *eqEnv
	eqFuzzHistory [][]byte
)

// runEqFuzz runs one input and returns the fast path's hits.
func runEqFuzz(t *testing.T, data []byte) int {
	eqFuzzMu.Lock()
	defer eqFuzzMu.Unlock()
	if eqFuzzEnv == nil || len(eqFuzzHistory) >= eqFuzzInputs {
		resetEqFuzz()
		eqFuzzEnv = newEqEnv(t, 0, 16)
	}
	e := eqFuzzEnv
	eqFuzzHistory = append(eqFuzzHistory, bytes.Clone(data))
	t.Cleanup(func() {
		if t.Failed() {
			saveEqFuzzHistory(t)
			resetEqFuzz()
		}
	})
	c := fuzzutil.New(data)
	e.t, e.rng = t, consumerRand{c}
	hits := e.hits
	for range 1 + c.Intn(3) {
		e.round(c.Intn(66)) // the round number picks the param and max gas changes
	}
	return e.hits - hits
}

// eqFuzzHistoryDir holds the input histories of failed fuzz runs, one hex input
// per line. It is not under testdata/fuzz, where Go would read them as corpus.
const eqFuzzHistoryDir = "testdata/fast-recheck-history"

// saveEqFuzzHistory writes the inputs since the app pair was built, so the
// failure reproduces from a fresh pair.
func saveEqFuzzHistory(t *testing.T) {
	var sb strings.Builder
	for _, in := range eqFuzzHistory {
		sb.WriteString(hex.EncodeToString(in) + "\n")
	}
	sum := sha256.Sum256([]byte(sb.String()))
	path := filepath.Join(eqFuzzHistoryDir, hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(eqFuzzHistoryDir, 0o755); err == nil {
		err = os.WriteFile(path, []byte(sb.String()), 0o644)
		if err == nil {
			t.Logf("inputs since the app pair was built saved to %s", path)
			return
		}
	}
	t.Logf("inputs since the app pair was built:\n%s", sb.String())
}

// TestFastRecheckDifferentialHistories replays each saved history on a fresh app pair.
func TestFastRecheckDifferentialHistories(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(eqFuzzHistoryDir, "*"))
	require.NoError(t, err)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			raw, err := os.ReadFile(f)
			require.NoError(t, err)
			eqFuzzMu.Lock()
			resetEqFuzz()
			eqFuzzMu.Unlock()
			for line := range strings.FieldsSeq(string(raw)) {
				in, err := hex.DecodeString(line)
				require.NoError(t, err)
				runEqFuzz(t, in)
			}
		})
	}
}

func resetEqFuzz() {
	if eqFuzzEnv != nil {
		eqFuzzEnv.full.Close()
		eqFuzzEnv.fast.Close()
	}
	eqFuzzEnv, eqFuzzHistory = nil, nil
}

// consumerRand reads eqEnv's random choices from fuzz input, zero once it is
// exhausted.
type consumerRand struct{ c *fuzzutil.Consumer }

func (r consumerRand) Intn(n int) int { return r.c.Intn(n) }

func (r consumerRand) Perm(n int) []int {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := r.c.Intn(i + 1)
		p[i], p[j] = p[j], p[i]
	}
	return p
}

func (r consumerRand) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.c.Byte()
	}
	return len(p), nil
}

// eqFuzzSeeds are fixed pseudo-random inputs long enough for three rounds.
func eqFuzzSeeds() [][]byte {
	seeds := make([][]byte, 4)
	for i := range seeds {
		seeds[i] = make([]byte, 16<<10)
		rand.New(rand.NewSource(int64(i))).Read(seeds[i])
	}
	return seeds
}
