package app_test

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	blobv4 "github.com/celestiaorg/go-square/v4/proto/blob/v4"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// TestPrepareProposalDifferentialLazyBlob logs the same digest as TestPrepareProposalDifferential
// for malformed blob txs around the PFB cap, blob envelope look-alikes and PFFs with duplicated fields.
func TestPrepareProposalDifferentialLazyBlob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping prepare proposal differential test in short mode.")
	}

	var (
		numValid     = 2*appconsts.MaxPFBMessages + 50
		numMalformed = 100
		numLookalike = 6
		numSends     = 20
		numPFFs      = 20
		numPFFDups   = 4 * 10
	)
	accounts := testfactory.GenerateAccounts(numValid + numMalformed + numLookalike + numSends + numPFFs + numPFFDups)
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	signerAccounts := make([]*user.Account, len(accounts))
	for i, info := range queryAccountInfo(testApp, accounts, kr) {
		signerAccounts[i] = user.NewAccount(accounts[i], info.AccountNum, info.Sequence)
	}
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, signerAccounts...)
	require.NoError(t, err)

	next := 0
	nextAccount := func() (string, sdk.AccAddress) {
		name := accounts[next]
		next++
		return name, testfactory.GetAddress(kr, name)
	}
	ns := share.MustNewV0Namespace([]byte("diff-lazy"))
	blobTx := func() []byte {
		name, _ := nextAccount()
		blob, err := share.NewV0Blob(ns, []byte(fmt.Sprintf("blob %d", next)))
		require.NoError(t, err)
		raw, _, err := signer.CreatePayForBlobs(name, []*share.Blob{blob}, user.SetGasLimit(300_000), user.SetFee(30_000))
		require.NoError(t, err)
		return raw
	}
	sendTx := func() []byte {
		_, addr := nextAccount()
		raw, _, err := signer.CreateTx([]sdk.Msg{banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))},
			user.SetGasLimit(100_000), user.SetFee(10_000))
		require.NoError(t, err)
		return raw
	}
	marshal := func(m proto.Message) []byte {
		bz, err := proto.Marshal(m)
		require.NoError(t, err)
		return bz
	}

	valid := make([][]byte, numValid)
	for i := range valid {
		valid[i] = blobTx()
	}
	// malformed blob txs are non-canonical: proto.Unmarshal accepts the padding, UnmarshalBlobTx does not.
	malformed := make([][]byte, numMalformed)
	for i := range malformed {
		malformed[i] = appendUnknownProtoField(blobTx(), 16)
	}

	// lookalikes carry the BLOB type ID but are not valid blob txs.
	inner, _, err := blobtx.UnmarshalBlobTx(blobTx())
	require.NoError(t, err)
	data := []byte("lookalike")
	unknownInBlob := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), inner.Tx)
	unknownInBlob = protowire.AppendBytes(protowire.AppendTag(unknownInBlob, 2, protowire.BytesType),
		appendUnknownProtoField(marshal(&blobv4.BlobProto{NamespaceId: ns.ID(), Data: data}), 4))
	unknownInBlob = protowire.AppendString(protowire.AppendTag(unknownInBlob, 3, protowire.BytesType), blobtx.ProtoBlobTxTypeID)
	nested, err := blobtx.MarshalBlobTx(valid[0], inner.Blobs...)
	require.NoError(t, err)
	var sigBLOB txtypes.TxRaw
	require.NoError(t, sigBLOB.Unmarshal(sendTx()))
	sigBLOB.Signatures = [][]byte{[]byte(blobtx.ProtoBlobTxTypeID)}
	sigBLOBTx, err := sigBLOB.Marshal()
	require.NoError(t, err)
	lookalikes := [][]byte{
		marshal(&blobv4.BlobTx{Tx: inner.Tx, TypeId: blobtx.ProtoBlobTxTypeID}),
		marshal(&blobv4.BlobTx{Tx: inner.Tx, Blobs: []*blobv4.BlobProto{{NamespaceId: []byte{1}, Data: data}}, TypeId: blobtx.ProtoBlobTxTypeID}),
		marshal(&blobv4.BlobTx{Tx: inner.Tx, Blobs: []*blobv4.BlobProto{{NamespaceId: ns.ID(), ShareVersion: 99, Data: data}}, TypeId: blobtx.ProtoBlobTxTypeID}),
		unknownInBlob,
		nested,
		// A TxRaw whose only signature is "BLOB" reads as a BlobTx wrapper.
		sigBLOBTx,
	}
	for i, tx := range lookalikes {
		_, isBlob, err := blobtx.UnmarshalBlobTx(tx)
		require.True(t, isBlob, "lookalike %d", i)
		require.Error(t, err, "lookalike %d", i)
	}

	sends := make([][]byte, numSends)
	for i := range sends {
		sends[i] = sendTx()
	}
	pffTx := func() ([]byte, string) {
		name, addr := nextAccount()
		seedFibreEscrow(t, testApp, addr, 1_000_000)
		return newSignedPayForFibreTx(t, signer, name, true), name
	}
	pffs := make([][]byte, numPFFs)
	for i := range pffs {
		pffs[i], _ = pffTx()
	}

	// resign replaces the body of rawTx and signs it again for account.
	resign := func(rawTx []byte, account string, body []byte) []byte {
		var raw txtypes.TxRaw
		require.NoError(t, raw.Unmarshal(rawTx))
		doc := txtypes.SignDoc{BodyBytes: body, AuthInfoBytes: raw.AuthInfoBytes, ChainId: testutil.ChainID, AccountNumber: signer.Account(account).AccountNumber()}
		signBytes, err := doc.Marshal()
		require.NoError(t, err)
		sig, _, err := kr.Sign(account, signBytes, signing.SignMode_SIGN_MODE_DIRECT)
		require.NoError(t, err)
		raw.BodyBytes, raw.Signatures = body, [][]byte{sig}
		out, err := raw.Marshal()
		require.NoError(t, err)
		return out
	}
	bytesField := func(num protowire.Number, v []byte) []byte {
		return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), v)
	}
	// bodyWithTypeURLs is rawTx's body with its message Any carrying both type URLs.
	bodyWithTypeURLs := func(rawTx []byte, first, last string, value []byte) []byte {
		var raw txtypes.TxRaw
		require.NoError(t, raw.Unmarshal(rawTx))
		var body txtypes.TxBody
		require.NoError(t, body.Unmarshal(raw.BodyBytes))
		body.Messages = nil
		rest, err := body.Marshal()
		require.NoError(t, err)
		anyBz := append(append(bytesField(1, []byte(first)), bytesField(1, []byte(last))...), bytesField(2, value)...)
		return append(bytesField(1, anyBz), rest...)
	}
	pffTypeURL := sdk.MsgTypeURL(&fibretypes.MsgPayForFibre{})
	sendTypeURL := sdk.MsgTypeURL(&banktypes.MsgSend{})
	var sendRaw txtypes.TxRaw
	require.NoError(t, sendRaw.Unmarshal(sends[0]))
	pffDups := make([][]byte, 0, numPFFDups)
	for range numPFFDups / 4 {
		// A decoder keeps the last body_bytes: an earlier send body must not hide the PFF.
		tx, _ := pffTx()
		pffDups = append(pffDups, append(bytesField(1, sendRaw.BodyBytes), tx...))
		// ...and a later send body hides it (the PFF signature no longer matches).
		tx, _ = pffTx()
		var raw txtypes.TxRaw
		require.NoError(t, raw.Unmarshal(tx))
		pffBody := raw.BodyBytes
		raw.BodyBytes = nil
		rest, err := raw.Marshal()
		require.NoError(t, err)
		pffDups = append(pffDups, slices.Concat(bytesField(1, pffBody), bytesField(1, sendRaw.BodyBytes), rest))
		// A decoder keeps the last type_url of an Any.
		tx, name := pffTx()
		raw = txtypes.TxRaw{}
		require.NoError(t, raw.Unmarshal(tx))
		var body txtypes.TxBody
		require.NoError(t, body.Unmarshal(raw.BodyBytes))
		pffDups = append(pffDups, resign(tx, name, bodyWithTypeURLs(tx, sendTypeURL, pffTypeURL, body.Messages[0].Value)))
		tx, name = pffTx()
		addr := signer.Account(name).Address()
		sendValue, err := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1))).Marshal()
		require.NoError(t, err)
		pffDups = append(pffDups, resign(tx, name, bodyWithTypeURLs(tx, pffTypeURL, sendTypeURL, sendValue)))
	}

	// every inserts one tx of b after every k txs of a.
	every := func(a, b [][]byte, k int) [][]byte {
		out := make([][]byte, 0, len(a)+len(b))
		j := 0
		for i, tx := range a {
			out = append(out, tx)
			if (i+1)%k == 0 && j < len(b) {
				out = append(out, b[j])
				j++
			}
		}
		return append(out, b[j:]...)
	}

	workloads := []struct {
		name     string
		txs      [][]byte
		wantKept int
	}{
		// One malformed tx after every 4 valid ones: 50 before the cap, 50 after it.
		{"blob-malformed-around-cap", every(valid, malformed, 4), appconsts.MaxPFBMessages},
		{"blob-malformed-after-cap", slices.Concat(valid, malformed), appconsts.MaxPFBMessages},
		{"blob-malformed-before-cap", slices.Concat(malformed, valid), appconsts.MaxPFBMessages},
		{"blob-envelope-lookalikes", slices.Concat(lookalikes, valid[:50], sends[:10], lookalikes), 60},
		// Per group of 4 dups: the first and third are PFFs, the fourth is a MsgSend.
		{"pff-duplicated-fields", every(pffs, pffDups, 1), numPFFs + 3*numPFFDups/4},
		{"all-mixed", every(slices.Concat(valid, pffs, sends), slices.Concat(malformed, lookalikes, pffDups), 3), appconsts.MaxPFBMessages + numPFFs + numSends + 3*numPFFDups/4},
	}
	for _, w := range workloads {
		t.Run(w.name, func(t *testing.T) {
			index := make(map[string]int, len(w.txs))
			for i, tx := range w.txs {
				index[string(tx)] = i
			}

			height := testApp.LastBlockHeight() + 1
			blockTime := time.Now()
			prepared, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{Height: height, Time: blockTime, Txs: w.txs})
			require.NoError(t, err)

			kept := make([]string, len(prepared.Txs))
			for i, tx := range prepared.Txs {
				idx, ok := index[string(tx)]
				require.True(t, ok, "output tx %d is not an input", i)
				kept[i] = fmt.Sprint(idx)
			}
			digest := sha256.Sum256([]byte(fmt.Sprintf("%d|%s", prepared.SquareSize, strings.Join(kept, ","))))
			t.Logf("DIGEST %s in=%d kept=%d square=%d sha256=%x", w.name, len(w.txs), len(prepared.Txs), prepared.SquareSize, digest)
			require.Len(t, prepared.Txs, w.wantKept)

			processed, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
				Height: height, Time: blockTime, Txs: prepared.Txs,
				SquareSize: prepared.SquareSize, DataRootHash: prepared.DataRootHash,
			})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processed.Status)
		})
	}
}
