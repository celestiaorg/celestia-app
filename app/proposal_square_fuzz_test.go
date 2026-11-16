package app

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/da"
	"github.com/celestiaorg/celestia-app/v11/pkg/wrapper"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	fibrekeeper "github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/stretchr/testify/require"
)

// proposalSquareTxWidth is the fixed width of one tx record: kind, a blob
// count, a namespace byte and a data length.
const proposalSquareTxWidth = 5

// FuzzBuildProposalSquareParity checks the square ProcessProposal builds from
// a proposer's txs against the independent pkg/da construction. The block mixes
// valid signed blob txs, blob txs with arbitrary inner bytes, plain txs and raw
// fuzz bytes, decoded as ProcessProposal decodes them, under any power-of-two
// max square size. buildProposalSquare must fail exactly when da.ConstructEDS
// fails, must report the same square size, and given that size must produce
// the same data root. It must never recover a panic.
func FuzzBuildProposalSquareParity(f *testing.F) {
	rec := func(kind, count, ns byte, size uint16) []byte {
		return []byte{kind, count, ns, byte(size >> 8), byte(size)}
	}
	f.Add(uint8(6), fuzzutil.Records(proposalSquareTxWidth, rec(0, 1, 1, 600)), []byte{})
	f.Add(uint8(6), fuzzutil.Records(proposalSquareTxWidth, rec(0, 2, 7, 3000), rec(2, 0, 0, 200), rec(0, 1, 2, 1)), []byte("tx"))
	f.Add(uint8(0), fuzzutil.Records(proposalSquareTxWidth, rec(0, 3, 9, 4000)), []byte{})
	f.Add(uint8(3), fuzzutil.Records(proposalSquareTxWidth, rec(1, 1, 3, 100), rec(3, 0, 0, 0)), []byte("BLOB\x0a\x00"))
	f.Add(uint8(7), fuzzutil.Records(proposalSquareTxWidth, rec(2, 0, 0, 1)), bytes.Repeat([]byte{0xFF}, 40))

	fx := newTxCacheFuzzFixture(f)
	treePool, err := wrapper.DefaultPreallocatedTreePool(uint(appconsts.SquareSizeUpperBound))
	require.NoError(f, err)
	app := &App{encodingConfig: fx.enc, treePool: treePool}
	decoder := fx.enc.TxConfig.TxDecoder()
	f.Fuzz(func(t *testing.T, maxSquareLog uint8, records, raw []byte) {
		if len(raw) > 4*maxFuzzBlobData {
			return
		}
		maxSquare := 1 << (int(maxSquareLog) % 8) // 1..128
		var txs [][]byte
		c := fuzzutil.New(records)
		for len(txs) < 8 && !c.Exhausted() {
			r := c.Record(proposalSquareTxWidth)
			kind, count, nsByte, size := r.Byte()%4, int(r.Byte()%3)+1, r.Byte(), int(r.Uint16())%maxFuzzBlobData+1
			switch kind {
			case 0, 1: // a valid signed blob tx; kind 1 replaces its inner tx bytes
				blobs := make([]*share.Blob, 0, count)
				for i := range count {
					b, err := fx.blob([]byte{nsByte, byte(i)}, bytes.Repeat([]byte{nsByte ^ byte(i) | 1}, size), i%2 == 1)
					require.NoError(t, err)
					blobs = append(blobs, b)
				}
				btx, ok := fx.signedBlobTx(blobs)
				require.True(t, ok)
				inner := btx.Tx
				if kind == 1 {
					inner = raw
				}
				wire, err := blobtx.MarshalBlobTx(inner, btx.Blobs...)
				require.NoError(t, err)
				txs = append(txs, wire)
			case 2: // plain tx bytes
				txs = append(txs, bytes.Repeat([]byte{nsByte}, size%512+1))
			case 3: // raw fuzz bytes
				txs = append(txs, bytes.Clone(raw))
			}
		}

		// Decode as ProcessProposal does; a malformed blob tx rejects the
		// proposal before the square is built.
		blobTxs := make([]*blobtx.BlobTx, len(txs))
		decoded := make([]*fibretypes.DecodedPayForFibre, len(txs))
		for i, tx := range txs {
			btx, isBlob, err := blobtx.UnmarshalBlobTxNoCopy(tx)
			if isBlob {
				if err != nil {
					return
				}
				blobTxs[i] = btx
				continue
			}
			if !pffDecodeCandidate(tx) {
				continue
			}
			if sdkTx, err := decoder(tx); err == nil {
				if msg := fibretypes.PayForFibreMsg(sdkTx); msg != nil {
					decoded[i] = fibrekeeper.DerivePayForFibre(tx, msg)
				}
			}
		}

		var cancel atomic.Bool
		got := app.buildProposalSquare(txs, blobTxs, decoded, maxSquare, 0, &cancel)
		if got.err != nil {
			require.False(t, strings.Contains(got.err.Error(), "panic"), "buildProposalSquare recovered a panic: %v", got.err)
		}
		eds, wantErr := da.ConstructEDS(txs, appconsts.Version, maxSquare)
		require.Equal(t, wantErr == nil, got.err == nil, "build error %v, reference error %v", got.err, wantErr)
		if wantErr != nil {
			return
		}
		wantSize := uint64(eds.Width()) / 2
		require.Equal(t, wantSize, got.size)

		got = app.buildProposalSquare(txs, blobTxs, decoded, maxSquare, wantSize, &cancel)
		require.NoError(t, got.err)
		dah, err := da.NewDataAvailabilityHeader(eds)
		require.NoError(t, err)
		require.Equal(t, dah.Hash(), got.root, "data root differs from the reference construction")
	})
}
