package types_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v11/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/stretchr/testify/require"
)

// blobTxSeeds returns valid blob txs: one blob, two blobs in different
// namespaces, a share version one blob, and a blob that spans many shares.
func blobTxSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr, _ := testnode.NewKeyring(testfactory.TestAccName)
	signer, err := user.NewSigner(kr, encCfg.TxConfig, testfactory.ChainID, user.NewAccount(testfactory.TestAccName, 1, 0))
	require.NoError(tb, err)
	addr := signer.Account(testfactory.TestAccName).Address()

	ns1 := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	ns2 := share.MustNewV0Namespace(bytes.Repeat([]byte{2}, share.NamespaceVersionZeroIDSize))
	v0, err := share.NewBlob(ns1, bytes.Repeat([]byte{0xaa}, 100), share.ShareVersionZero, nil)
	require.NoError(tb, err)
	v0b, err := share.NewBlob(ns2, bytes.Repeat([]byte{0xbb}, 700), share.ShareVersionZero, nil)
	require.NoError(tb, err)
	v1, err := share.NewBlob(ns1, bytes.Repeat([]byte{0xcc}, 50), share.ShareVersionOne, addr.Bytes())
	require.NoError(tb, err)
	big, err := share.NewBlob(ns2, bytes.Repeat([]byte{0xdd}, 20_000), share.ShareVersionZero, nil)
	require.NoError(tb, err)

	blobSets := [][]*share.Blob{{v0}, {v0, v0b}, {v1}, {big}}
	seeds := make([][]byte, 0, len(blobSets))
	for _, blobs := range blobSets {
		raw, _, err := signer.CreatePayForBlobs(testfactory.TestAccName, blobs, user.SetGasLimit(1_000_000), user.SetFee(1_000))
		require.NoError(tb, err)
		seeds = append(seeds, raw)
	}
	return seeds
}

// validateBlobTxBytes decodes raw as a blob tx and runs both validation paths.
// Neither may panic, and the cached path (ValidateBlobTxSkipCommitment) must
// never accept a tx that the full path rejects for any reason other than a
// commitment mismatch: ProcessProposal trusts the cached path after CheckTx.
func validateBlobTxBytes(t *testing.T, encCfg encoding.Config, raw []byte) {
	t.Helper()
	bTx, isBlob, err := blobtx.UnmarshalBlobTx(raw)
	if !isBlob || err != nil {
		return
	}
	_, skipErr := types.ValidateBlobTxSkipCommitment(encCfg.TxConfig, bTx)
	fullErr := types.ValidateBlobTx(encCfg.TxConfig, bTx, appconsts.SubtreeRootThreshold, appconsts.Version)
	if skipErr != nil {
		require.Error(t, fullErr, "cached validation rejected a tx the full validation accepts")
		return
	}
	if fullErr != nil {
		require.ErrorIs(t, fullErr, types.ErrInvalidShareCommitment,
			"full validation rejected a tx the cached validation accepts for a reason other than the commitment")
	}
}

func TestValidateBlobTxSeeds(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	for _, raw := range blobTxSeeds(t) {
		bTx, isBlob, err := blobtx.UnmarshalBlobTx(raw)
		require.True(t, isBlob)
		require.NoError(t, err)
		require.NoError(t, types.ValidateBlobTx(encCfg.TxConfig, bTx, appconsts.SubtreeRootThreshold, appconsts.Version))
		validateBlobTxBytes(t, encCfg, raw)
	}
}

// FuzzValidateBlobTx feeds mutated blob tx bytes through the validation that
// CheckTx and ProcessProposal run on every blob tx.
func FuzzValidateBlobTx(f *testing.F) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	for _, raw := range blobTxSeeds(f) {
		f.Add(raw)
	}
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x00})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > appconsts.MaxTxSize {
			return
		}
		validateBlobTxBytes(t, encCfg, raw)
	})
}

// FuzzMsgPayForBlobsValidateBasic checks that stateless message validation,
// and gas estimation of the messages it accepts, never panic on inconsistent
// namespaces, sizes and commitments. The field
// counts come from the fuzzer, so the slices can disagree in length.
func FuzzMsgPayForBlobsValidateBasic(f *testing.F) {
	f.Add("celestia1y6ykdv9d4txs0ysvz0wf6v4hz8njlv3s3ecfzz", uint8(1), uint8(1), uint8(1), uint8(1), uint32(100), []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, byte(0))
	f.Add("celestia1y6ykdv9d4txs0ysvz0wf6v4hz8njlv3s3ecfzz", uint8(2), uint8(1), uint8(2), uint8(2), uint32(0), []byte{}, byte(1))
	f.Add("", uint8(0), uint8(0), uint8(0), uint8(0), uint32(1<<31), bytes.Repeat([]byte{0xff}, 29), byte(255))
	f.Fuzz(func(t *testing.T, signer string, nsCount, commitCount, sizeCount, versionCount uint8, size uint32, ns []byte, version byte) {
		msg := &types.MsgPayForBlobs{Signer: signer}
		for range nsCount {
			msg.Namespaces = append(msg.Namespaces, ns)
		}
		for range commitCount {
			msg.ShareCommitments = append(msg.ShareCommitments, bytes.Repeat([]byte{version}, 32))
		}
		for range sizeCount {
			msg.BlobSizes = append(msg.BlobSizes, size)
		}
		for range versionCount {
			msg.ShareVersions = append(msg.ShareVersions, uint32(version))
		}
		// Gas estimation assumes a message that passed ValidateBasic, as it
		// does in the ante handler, so only estimate for valid messages.
		if msg.ValidateBasic() == nil {
			_ = types.DefaultEstimateGas(msg)
		}
	})
}
