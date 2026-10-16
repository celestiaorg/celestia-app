package rsema1d

import (
	"bytes"
	"math/rand/v2"
	"sync"
	"testing"
)

func TestSplitParts(t *testing.T) {
	// 4096 and 6144 B/row (16 and 24 MiB blobs) split only where
	// minSplitRowBytes permits: on arm64 (floor = minSplitBytes) they split,
	// off arm64 (floor 8192) they do not. Derive the expectation from the gate
	// so the table holds on either architecture.
	want4096, want6144 := 2, 2
	if minSplitRowBytes > 4096 {
		want4096 = 1
	}
	if minSplitRowBytes > 6144 {
		want6144 = 1
	}
	for _, tc := range []struct{ rowSize, workers, want int }{
		{32768, 16, 16},
		{32768, 32, 16},
		{32768, 5, 4},
		{32768, 1, 1},
		{32768, 0, 1},
		{4096, 16, want4096},
		{2048, 16, 1},
		{64, 16, 1},
		{4160, 16, 1}, // 65 chunks: not evenly divisible
		{6144, 16, want6144},
		{8192, 16, 4}, // 32 MiB blob: at the amd64 floor, splits 4 ways on both arches
	} {
		if got := splitParts(tc.rowSize, tc.workers); got != tc.want {
			t.Errorf("splitParts(%d, %d) = %d, want %d", tc.rowSize, tc.workers, got, tc.want)
		}
	}
}

func TestEncodeParityMatchesSingle(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 11))
	for _, sh := range [][3]int{{4096, 12288, 32768}, {1024, 1024, 8192}, {32, 96, 6144}, {8, 8, 64}, {16, 48, 4160}} {
		k, n, rowSize := sh[0], sh[1], sh[2]
		if testing.Short() && k*rowSize > 1<<26 {
			continue
		}
		c, err := NewCoder(&Config{K: k, N: n, WorkerCount: 16})
		if err != nil {
			t.Fatal(err)
		}
		want := make([][]byte, k+n)
		got := make([][]byte, k+n)
		for i := range want {
			want[i] = make([]byte, rowSize)
			got[i] = make([]byte, rowSize)
			if i < k {
				for j := range want[i] {
					want[i][j] = byte(rng.Uint32())
				}
				copy(got[i], want[i])
			}
		}
		if err := c.enc.Encode(want); err != nil {
			t.Fatal(err)
		}
		if err := c.encodeParity(got); err != nil {
			t.Fatal(err)
		}
		for i := range want {
			if !bytes.Equal(want[i], got[i]) {
				t.Fatalf("k=%d n=%d rowSize=%d: row %d differs", k, n, rowSize, i)
			}
		}
	}
}

func TestEncodeRejectsUnequalRows(t *testing.T) {
	const k, n, rowSize = 4, 12, 4096
	c, err := NewCoder(&Config{K: k, N: n, WorkerCount: 16})
	if err != nil {
		t.Fatal(err)
	}
	rows := make([][]byte, k+n)
	for i := range rows {
		rows[i] = make([]byte, rowSize)
	}
	// A short row would panic inside a split worker goroutine; validateRows
	// must turn it into a returned error instead.
	rows[k+1] = make([]byte, rowSize-64)
	if _, err := c.Encode(rows); err == nil {
		t.Fatal("expected error for unequal row lengths, got nil")
	}
}

func TestEncodeConcurrent(t *testing.T) {
	const k, n, rowSize, encodes = 64, 192, 8192, 8
	c, err := NewCoder(&Config{K: k, N: n, WorkerCount: 16})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(seed uint64) [][]byte {
		rng := rand.New(rand.NewPCG(seed, 1))
		rows := make([][]byte, k+n)
		for i := range rows {
			rows[i] = make([]byte, rowSize)
			if i < k {
				for j := range rows[i] {
					rows[i][j] = byte(rng.Uint32())
				}
			}
		}
		return rows
	}
	want := make([]Commitment, encodes)
	for i := range encodes {
		ed, err := c.Encode(mk(uint64(i)))
		if err != nil {
			t.Fatal(err)
		}
		want[i] = ed.Commitment()
	}
	var wg sync.WaitGroup
	for i := range encodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ed, err := c.Encode(mk(uint64(i)))
			if err != nil || ed.Commitment() != want[i] {
				t.Errorf("encode %d: err=%v or commitment mismatch", i, err)
			}
		}()
	}
	wg.Wait()
}
