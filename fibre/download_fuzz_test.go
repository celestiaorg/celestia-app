package fibre

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/fibre/internal/row"
	"github.com/celestiaorg/celestia-app/v11/fibre/validator"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/rlc"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// downloadShardWidth is the fixed width of one served-shard record: the
// validator's expected rows, the first row index, the row count, a mutation
// and two mutation arguments.
const downloadShardWidth = 6

// FuzzDownload drives a client download session with up to eight validators
// that each serve one shard, honest or mutated: flipped row, proof or RLC
// bytes, wrong or out-of-range indices, resized rows and proofs, duplicate or
// missing rows. Shards go through the wire encoding, parseShard and addShard as
// in Client.downloadFrom, and Blob is called again after one refetch.
//
// The oracle: nothing panics; a returned blob carries exactly the original
// data; every shard buffer is released, and an accepted shard whose parity
// rows alias its buffer only after reconstruction; and when no shard was
// corrupted and the shards together cover K distinct rows, the download
// succeeds.
func FuzzDownload(f *testing.F) {
	shard := func(expected, start, count, mutation, a, b byte) []byte {
		return []byte{expected, start, count, mutation, a, b}
	}
	f.Add(fuzzutil.Records(downloadShardWidth, shard(8, 0, 8, 0, 0, 0)))
	f.Add(fuzzutil.Records(downloadShardWidth, shard(4, 0, 4, 0, 0, 0), shard(4, 8, 4, 0, 0, 0)))
	f.Add(fuzzutil.Records(downloadShardWidth, shard(8, 8, 8, 0, 0, 0)))
	f.Add(fuzzutil.Records(downloadShardWidth, shard(4, 0, 4, 1, 1, 2), shard(8, 4, 8, 0, 0, 0)))
	f.Add(fuzzutil.Records(downloadShardWidth, shard(4, 0, 4, 2, 0, 3), shard(8, 8, 8, 0, 0, 0)))
	f.Add(fuzzutil.Records(downloadShardWidth, shard(4, 0, 4, 3, 2, 0), shard(4, 4, 4, 4, 0, 0), shard(8, 8, 8, 0, 0, 0)))
	f.Add(fuzzutil.Records(downloadShardWidth, shard(8, 0, 8, 5, 1, 1), shard(8, 8, 8, 0, 0, 0)))
	f.Add(fuzzutil.Records(downloadShardWidth, shard(8, 0, 8, 6, 0, 9), shard(8, 8, 8, 0, 0, 0)))
	f.Add(fuzzutil.Records(downloadShardWidth, shard(8, 0, 8, 7, 0, 1), shard(8, 8, 8, 0, 0, 0)))
	f.Add(fuzzutil.Records(downloadShardWidth, shard(8, 0, 8, 8, 3, 0), shard(8, 8, 8, 9, 0, 0)))

	// One honest encoding with a valid blob header, built once.
	cfg := &rsema1d.Config{K: testK, N: testN, WorkerCount: 1}
	coder, err := rsema1d.NewCoder(cfg)
	require.NoError(f, err)
	r := rand.New(rand.NewPCG(7, 8))
	rows := make([][]byte, testK+testN)
	for i := range rows {
		rows[i] = make([]byte, testRowSize)
		if i < testK {
			for j := range rows[i] {
				rows[i][j] = byte(r.IntN(256))
			}
		}
	}
	dataSize := testK*testRowSize - blobHeaderLen - 17
	newBlobHeaderV0(dataSize).marshalTo(rows[0])
	ed, err := coder.Encode(rows)
	require.NoError(f, err)
	want := bytes.Join(rows[:testK], nil)[blobHeaderLen : blobHeaderLen+dataSize]
	honest := make([]*types.BlobRow, testK+testN)
	for i := range honest {
		p, err := ed.GenerateRowProof(i)
		require.NoError(f, err)
		honest[i] = &types.BlobRow{Index: uint32(i), Data: p.Row, Proof: p.RowProof}
	}
	rlcs := rlc.Marshal(ed.RLC())
	id := NewBlobID(0, Commitment(ed.Commitment()))
	vals := make([]*core.Validator, 8)
	for i := range vals {
		priv := cmted25519.GenPrivKey()
		vals[i] = &core.Validator{Address: priv.PubKey().Address(), PubKey: priv.PubKey(), VotingPower: 1}
	}

	f.Fuzz(func(t *testing.T, records []byte) {
		c := fuzzutil.New(records)
		var shards []*types.BlobShard
		var selected []validator.SelectedValidator
		clean := true
		covered := map[uint32]bool{}
		for len(shards) < len(vals) && !c.Exhausted() {
			rec := c.Record(downloadShardWidth)
			expected, start, count := int(rec.Byte()%16)+1, int(rec.Byte()), int(rec.Byte()%17)
			mutation, a, b := rec.Byte()%10, rec.Byte(), rec.Byte()
			s := &types.BlobShard{Rlcs: bytes.Clone(rlcs)}
			for i := range count {
				src := honest[(start+i)%len(honest)]
				s.Rows = append(s.Rows, &types.BlobRow{Index: src.Index, Data: bytes.Clone(src.Data), Proof: append([][]byte(nil), src.Proof...)})
			}
			if mutation != 0 && mutateDownloadShard(s, mutation, int(a), b) {
				clean = false
			}
			if clean {
				for _, row := range s.Rows {
					covered[row.Index] = true
				}
			}
			shards = append(shards, s)
			selected = append(selected, validator.SelectedValidator{Validator: vals[len(selected)], ExpectedRows: expected})
		}

		blobCfg := BlobConfig{
			OriginalRows: testK,
			ParityRows:   testN,
			MaxDataSize:  testK*testRowSize - blobHeaderLen,
			MaxRowSize:   testRowSize,
			Coder:        coder,
			DataPool:     row.NewPool(testRowSize, testK),
		}
		d, err := newDownload(blobCfg, id, selected)
		require.NoError(t, err)
		defer d.freeSlab()

		ctx := context.Background()
		released := make([]int, len(shards))
		retainedOK := make([]bool, len(shards)) // accepted shards whose parity rows alias the buffer
		next := 0
		var blob *Blob
		for attempt := 0; ; attempt++ {
			require.Less(t, attempt, 2, "Blob asked to refetch twice")
			for from := range d.ShardSources(ctx) {
				i := next
				next++
				release := func() { released[i]++ }
				// Send the shard through the wire encoding, as a server would.
				wire, err := shards[i].Marshal()
				require.NoError(t, err)
				var decoded types.BlobShard
				require.NoError(t, decoded.Unmarshal(wire))
				proofs, vec, err := parseShard(&decoded, testK)
				if err != nil {
					release()
					d.SkipShard(from)
					continue
				}
				retained, err := addShard(d, from, proofs, vec, release)
				if err != nil {
					d.SkipShard(from)
					continue
				}
				if !retained {
					release()
					continue
				}
				retainedOK[i] = true
			}
			for i := range next {
				if retainedOK[i] {
					require.Zero(t, released[i], "shard %d buffer released before reconstruction", i)
				}
			}
			blob, err = d.Blob(ctx)
			if !errors.Is(err, errRefetch) {
				break
			}
		}

		// Release is idempotent, so a rejected shard may be released twice, but
		// every buffer must go back to the pool.
		for i := range next {
			require.Positive(t, released[i], "shard %d buffer never released", i)
		}
		if err != nil {
			require.Nil(t, blob)
			if clean && len(covered) >= testK {
				t.Fatalf("download of %d distinct honest rows failed: %v", len(covered), err)
			}
			return
		}
		require.Equal(t, want, blob.Data(), "download returned data that is not the committed blob")
		blob.Free()
	})
}

// mutateDownloadShard corrupts s and reports whether it changed anything.
func mutateDownloadShard(s *types.BlobShard, mutation byte, a int, b byte) bool {
	if len(s.Rows) == 0 && mutation < 7 {
		return false
	}
	var row *types.BlobRow
	if len(s.Rows) > 0 {
		row = s.Rows[a%len(s.Rows)]
	}
	switch mutation {
	case 1: // flip a row byte
		row.Data = bytes.Clone(row.Data)
		row.Data[int(b)%len(row.Data)] ^= 1 << (b % 8)
	case 2: // flip a proof byte
		if len(row.Proof) == 0 {
			return false
		}
		seg := bytes.Clone(row.Proof[int(b)%len(row.Proof)])
		seg[int(b)%len(seg)] ^= 0x80
		row.Proof[int(b)%len(row.Proof)] = seg
	case 3: // move the row to another index, possibly out of range
		row.Index = uint32(int(b) % (2 * (testK + testN)))
	case 4: // resize the row, including oversizing it
		if b&1 == 0 {
			row.Data = row.Data[:int(b)%len(row.Data)]
		} else {
			row.Data = append(bytes.Clone(row.Data), make([]byte, int(b))...)
		}
	case 5: // drop or add a proof segment
		if b&1 == 0 && len(row.Proof) > 0 {
			row.Proof = row.Proof[:len(row.Proof)-1]
		} else {
			row.Proof = append(row.Proof, make([]byte, 32))
		}
	case 6: // duplicate a row in the same shard; this changes nothing served
		s.Rows = append(s.Rows, row)
		return false
	case 7: // flip an RLC byte
		s.Rlcs[int(b)%len(s.Rlcs)] ^= 1 << (b % 8)
	case 8: // resize the RLCs
		if b&1 == 0 {
			s.Rlcs = s.Rlcs[:int(b)%len(s.Rlcs)]
		} else {
			s.Rlcs = append(s.Rlcs, make([]byte, 16)...)
		}
	case 9: // serve no rows
		s.Rows = nil
	}
	return true
}
