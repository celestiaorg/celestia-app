package rsema1d

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/klauspost/reedsolomon"
)

var reconstructShapes = [][3]int{{4096, 12288, 32768}, {1024, 1024, 8192}, {32, 96, 6144}, {8, 8, 64}, {16, 48, 4160}}

// encodedRows returns K+N encoded rows of deterministic data.
func encodedRows(t testing.TB, c *Coder, rowSize int, seed uint64) [][]byte {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, 1))
	rows := make([][]byte, c.config.K+c.config.N)
	for i := range rows {
		rows[i] = make([]byte, rowSize)
		if i < c.config.K {
			for j := range rows[i] {
				rows[i][j] = byte(rng.Uint32())
			}
		}
	}
	if err := c.enc.Encode(rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

// presentPatterns returns K-row selections of the K+N extended rows.
func presentPatterns(k, n int, seed uint64) map[string][]bool {
	rng := rand.New(rand.NewPCG(seed, 2))
	random := make([]bool, k+n)
	for _, idx := range rng.Perm(k + n)[:k] {
		random[idx] = true
	}
	allParity := make([]bool, k+n)
	for i := k; i < 2*k; i++ {
		allParity[i] = true
	}
	allOriginals := make([]bool, k+n)
	for i := range k {
		allOriginals[i] = true
	}
	quarter := make([]bool, k+n)
	for i := range k / 4 {
		quarter[i] = true
	}
	for i := k; i < k+k-k/4; i++ {
		quarter[i] = true
	}
	return map[string][]bool{
		"all_parity":        allParity,
		"random":            random,
		"quarter_originals": quarter,
		"all_originals":     allOriginals,
	}
}

// selectRows builds the caller-facing row slice: present rows alias full, missing
// originals are empty slab-backed slots (like fibre), missing parity are nil.
func selectRows(full [][]byte, present []bool, k, rowSize int) [][]byte {
	slab := make([]byte, k*rowSize)
	rows := make([][]byte, len(full))
	for i := range rows {
		switch {
		case present[i]:
			rows[i] = full[i]
		case i < k:
			off := i * rowSize
			rows[i] = slab[off : off : off+rowSize]
		}
	}
	return rows
}

func TestReconstructDataMatchesSingle(t *testing.T) {
	for _, sh := range reconstructShapes {
		k, n, rowSize := sh[0], sh[1], sh[2]
		if testing.Short() && k*rowSize > 1<<26 {
			continue
		}
		c, err := NewCoder(&Config{K: k, N: n, WorkerCount: 16})
		if err != nil {
			t.Fatal(err)
		}
		full := encodedRows(t, c, rowSize, uint64(k))
		for name, present := range presentPatterns(k, n, uint64(k)) {
			want := selectRows(full, present, k, rowSize)
			got := selectRows(full, present, k, rowSize)
			if err := c.enc.ReconstructData(want); err != nil {
				t.Fatal(err)
			}
			if err := c.reconstructData(got); err != nil {
				t.Fatal(err)
			}
			for i := range full {
				if (want[i] == nil) != (got[i] == nil) || len(want[i]) != len(got[i]) || !bytes.Equal(want[i], got[i]) {
					t.Fatalf("k=%d n=%d rowSize=%d %s: row %d differs", k, n, rowSize, name, i)
				}
				if i < k && !bytes.Equal(got[i], full[i]) {
					t.Fatalf("k=%d n=%d rowSize=%d %s: original row %d not recovered", k, n, rowSize, name, i)
				}
			}
			// recovered originals must land in the caller's slot, not a fresh allocation
			for i := range k {
				if !present[i] && cap(got[i]) != rowSize {
					t.Fatalf("%s: row %d was reallocated", name, i)
				}
			}
		}
	}
}

func TestReconstructDataAllocatesMissingSlots(t *testing.T) {
	const k, n, rowSize = 32, 96, 6144
	c, err := NewCoder(&Config{K: k, N: n, WorkerCount: 16})
	if err != nil {
		t.Fatal(err)
	}
	full := encodedRows(t, c, rowSize, 3)
	rows := make([][]byte, k+n)
	for i := k; i < 2*k; i++ {
		rows[i] = full[i]
	}
	rows[0] = make([]byte, 0, rowSize-1) // too small to reuse
	if err := c.reconstructData(rows); err != nil {
		t.Fatal(err)
	}
	for i := range k {
		if !bytes.Equal(rows[i], full[i]) {
			t.Fatalf("original row %d not recovered", i)
		}
	}
}

func TestReconstructDataErrorsMatchSingle(t *testing.T) {
	const k, n, rowSize = 32, 96, 6144
	c, err := NewCoder(&Config{K: k, N: n, WorkerCount: 16})
	if err != nil {
		t.Fatal(err)
	}
	full := encodedRows(t, c, rowSize, 4)
	present := presentPatterns(k, n, 4)["random"]
	cases := map[string]func() [][]byte{
		"too_few_rows": func() [][]byte {
			rows := selectRows(full, present, k, rowSize)
			for i := range rows {
				if present[i] {
					rows[i] = nil
					break
				}
			}
			return rows
		},
		"wrong_row_count": func() [][]byte { return selectRows(full, present, k, rowSize)[:k+n-1] },
		"mixed_sizes": func() [][]byte {
			rows := selectRows(full, present, k, rowSize)
			for i := range rows {
				if present[i] {
					rows[i] = rows[i][:rowSize-64]
					break
				}
			}
			return rows
		},
		"unaligned_size": func() [][]byte {
			rows := selectRows(full, present, k, rowSize)
			for i := range rows {
				if present[i] {
					rows[i] = rows[i][:rowSize-1]
				}
			}
			return rows
		},
		"no_data": func() [][]byte { return make([][]byte, k+n) },
	}
	for name, mk := range cases {
		want := c.enc.ReconstructData(mk())
		got := c.reconstructData(mk())
		if want == nil || !errors.Is(got, want) || got.Error() != want.Error() {
			t.Fatalf("%s: want %v, got %v", name, want, got)
		}
	}
	if err := c.reconstructData(cases["too_few_rows"]()); !errors.Is(err, reedsolomon.ErrTooFewShards) {
		t.Fatalf("expected ErrTooFewShards, got %v", err)
	}
}

func TestReconstructDataConcurrent(t *testing.T) {
	const k, n, rowSize, jobs = 64, 192, 8192, 8
	c, err := NewCoder(&Config{K: k, N: n, WorkerCount: 16})
	if err != nil {
		t.Fatal(err)
	}
	fulls := make([][][]byte, jobs)
	for i := range fulls {
		fulls[i] = encodedRows(t, c, rowSize, uint64(i))
	}
	patterns := presentPatterns(k, n, 9)
	var wg sync.WaitGroup
	for i := range jobs {
		for _, present := range patterns {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rows := selectRows(fulls[i], present, k, rowSize)
				if err := c.reconstructData(rows); err != nil {
					t.Error(err)
					return
				}
				for j := range k {
					if !bytes.Equal(rows[j], fulls[i][j]) {
						t.Errorf("job %d: row %d differs", i, j)
						return
					}
				}
			}()
		}
	}
	wg.Wait()
}
