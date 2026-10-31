package ante

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// fuzzBlobParams is a BlobKeeper with a fixed governance square size.
type fuzzBlobParams struct{ squareSize uint64 }

func (k fuzzBlobParams) GetParams(sdk.Context) blobtypes.Params {
	return blobtypes.Params{GasPerBlobByte: appconsts.GasPerBlobByte, GovMaxSquareSize: k.squareSize}
}

// excludedShortShareVersions reports a PFB with more blob sizes than share
// versions, which is excluded below.
func excludedShortShareVersions(msg *blobtypes.MsgPayForBlobs) bool {
	return len(msg.ShareVersions) < len(msg.BlobSizes)
}

// FuzzPFBShareAndGasValidation checks getSharesNeeded, validateEnoughGas and
// the decorator walks that call them on MsgPayForBlobs whose BlobSizes,
// ShareVersions and Namespaces vectors have arbitrary, possibly mismatched,
// lengths, as a reordered ante chain could pass them before ValidateBasic.
// They must return a result or an error, never panic, and agree with an
// independent big-integer share and gas computation.
func FuzzPFBShareAndGasValidation(f *testing.F) {
	sizes := func(vs ...uint32) []byte {
		out := make([]byte, 4*len(vs))
		for i, v := range vs {
			binary.BigEndian.PutUint32(out[4*i:], v)
		}
		return out
	}
	f.Add(sizes(1), []byte{0}, byte(1), uint32(200), uint64(1_000_000), uint64(64))
	f.Add(sizes(478, 479, 1<<20), []byte{0, 1, 0}, byte(3), uint32(1<<16), uint64(1<<40), uint64(128))
	f.Add(sizes(1<<32-1, 1<<32-1), []byte{1, 1}, byte(2), uint32(1<<32-1), uint64(0), uint64(512))
	f.Add(sizes(100, 200), []byte{0, 0, 0}, byte(0), uint32(0), uint64(1<<63), uint64(0))
	f.Add(sizes(), []byte{7}, byte(5), uint32(10), uint64(10), uint64(1))
	f.Add(sizes(8_000_000), []byte{0}, byte(1), uint32(500), uint64(65_536_000), uint64(64))

	f.Fuzz(func(t *testing.T, sizeBytes, versions []byte, namespaces byte, txSize uint32, txGas, squareSize uint64) {
		msg := &blobtypes.MsgPayForBlobs{ShareVersions: make([]uint32, 0, len(versions))}
		for i := 0; i+4 <= len(sizeBytes) && i < 4*64; i += 4 {
			msg.BlobSizes = append(msg.BlobSizes, binary.BigEndian.Uint32(sizeBytes[i:]))
		}
		for _, v := range versions[:min(len(versions), 64)] {
			msg.ShareVersions = append(msg.ShareVersions, uint32(v))
		}
		for range int(namespaces % 66) {
			msg.Namespaces = append(msg.Namespaces, make([]byte, share.NamespaceSize))
		}
		// Excluded pending a fix: https://linear.app/celestia/issue/PROTOCO-3525.
		if excludedShortShareVersions(msg) {
			return
		}

		// Independent computation: compact shares for the tx plus sparse
		// shares per blob, one more blob share when a signer is embedded.
		blobShares := new(big.Int)
		for i, size := range msg.BlobSizes {
			first := share.FirstSparseShareContentSize
			if msg.ShareVersions[i] == uint32(share.ShareVersionOne) {
				first -= share.SignerSize
			}
			n := int64(1)
			if size > uint32(first) {
				rest := int64(size) - int64(first)
				n += (rest + share.ContinuationSparseShareContentSize - 1) / share.ContinuationSparseShareContentSize
			}
			if size == 0 {
				n = 0
			}
			blobShares.Add(blobShares, big.NewInt(n))
		}
		wantShares := new(big.Int).Add(blobShares, big.NewInt(int64(share.CompactSharesNeeded(txSize))))
		if got := getSharesNeeded(txSize, msg); big.NewInt(int64(got)).Cmp(wantShares) != 0 {
			t.Fatalf("getSharesNeeded = %d, want %s", got, wantShares)
		}

		gasNeeded := new(big.Int).Mul(blobShares, big.NewInt(share.ShareSize*int64(appconsts.GasPerBlobByte)))
		enough := gasNeeded.Cmp(new(big.Int).SetUint64(txGas)) <= 0
		if err := validateEnoughGas(msg, appconsts.GasPerBlobByte, txGas); (err == nil) != enough {
			t.Fatalf("validateEnoughGas(gas %d) = %v, need %s", txGas, err, gasNeeded)
		}
		if err := NewMinGasPFBDecorator().validatePFBHasEnoughGas([]sdk.Msg{msg}, appconsts.GasPerBlobByte, txGas); (err == nil) != enough {
			t.Fatalf("validatePFBHasEnoughGas(gas %d) = %v, need %s", txGas, err, gasNeeded)
		}

		d := NewBlobShareDecorator(fuzzBlobParams{squareSize: squareSize})
		maxShares := new(big.Int).SetUint64(min(squareSize, uint64(appconsts.SquareSizeUpperBound)))
		maxShares.Mul(maxShares, maxShares)
		fits := wantShares.Cmp(maxShares) <= 0
		if err := d.validateMsgs([]sdk.Msg{msg}, txSize, int(maxShares.Int64())); (err == nil) != fits {
			t.Fatalf("validateMsgs = %v, need %s of %s shares", err, wantShares, maxShares)
		}
	})
}
