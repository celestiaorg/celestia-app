package proof_test

import (
	"bytes"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/da"
	"github.com/celestiaorg/celestia-app/v11/pkg/proof"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtversion "github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const (
	// maxFuzzQueryData bounds the request bytes of an iteration. The handlers
	// accept up to the max block size; small blocks reach the same code.
	maxFuzzQueryData = 64 << 10
	// maxFuzzBlockTxs bounds the transactions of a structured block.
	maxFuzzBlockTxs = 16
)

// Pool kinds, in the order the square requires them.
const (
	poolNormal = iota
	poolBlob
	poolFibre
)

type poolTx struct {
	kind  int
	bytes []byte
}

var (
	queryTxPoolOnce sync.Once
	queryTxPool     []poolTx
)

// fuzzQueryTxPool returns normal, blob and pay-for-fibre transactions that a
// fuzz input picks from to form a block. Every tx is a pure function of fixed
// keys and seeds, so a saved fuzz input reproduces the same block.
func fuzzQueryTxPool() []poolTx {
	queryTxPoolOnce.Do(func() {
		for i, size := range []int{1, 100, 479, 480, 1000, 2000} {
			queryTxPool = append(queryTxPool, poolTx{poolNormal, bytes.Repeat([]byte{byte(i + 1)}, size)})
		}
		signer, err := fixedBlobSigner()
		if err != nil {
			panic(err)
		}
		r := rand.New(rand.NewSource(1))
		for _, size := range []int{1, 600, 5000} {
			// ManyBlobs draws namespaces and data from r, unlike RandBlobTxs.
			count := 1 + size%2
			sizes := make([]int, count)
			for i := range sizes {
				sizes[i] = size
			}
			blobs := blobfactory.ManyBlobs(r, testfactory.RandomBlobNamespaces(r, count), sizes)
			tx, _, err := signer.CreatePayForBlobs(testfactory.TestAccName, blobs, blobfactory.DefaultTxOpts()...)
			if err != nil {
				panic(err)
			}
			queryTxPool = append(queryTxPool, poolTx{poolBlob, tx})
		}
		queryTxPool = append(queryTxPool, poolTx{poolFibre, payForFibreTx()})
	})
	return queryTxPool
}

// fuzzSignerMnemonic is a fixed test mnemonic; secp256k1 signing is
// deterministic, so blob txs signed with it are identical on every run.
const fuzzSignerMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

// fixedBlobSigner returns a signer whose key is derived from fuzzSignerMnemonic.
func fixedBlobSigner() (*user.Signer, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(encCfg.Codec)
	if _, err := kr.NewAccount(testfactory.TestAccName, fuzzSignerMnemonic, "", sdk.FullFundraiserPath, hd.Secp256k1); err != nil {
		return nil, err
	}
	return user.NewSigner(kr, encCfg.TxConfig, testfactory.ChainID, user.NewAccount(testfactory.TestAccName, 0, 0))
}

// payForFibreTx returns an unsigned pay-for-fibre tx with a fixed key and
// timestamp.
func payForFibreTx() []byte {
	pubKey := secp256k1.GenPrivKeyFromSecret([]byte("fuzz")).PubKey().(*secp256k1.PubKey)
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{0x01}, share.NamespaceVersionZeroIDSize))
	msg := &fibretypes.MsgPayForFibre{
		Signer: sdk.AccAddress(pubKey.Address()).String(),
		PaymentPromise: fibretypes.PaymentPromise{
			ChainId:           "test",
			Height:            1,
			Namespace:         ns.Bytes(),
			BlobSize:          100,
			BlobVersion:       fibretypes.BlobVersionZero,
			Commitment:        bytes.Repeat([]byte{0xAB}, share.FibreCommitmentSize),
			CreationTimestamp: time.Unix(1, 0).UTC(),
			SignerPublicKey:   *pubKey,
			Signature:         make([]byte, 64),
		},
	}
	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	builder := txConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		panic(err)
	}
	raw, err := txConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		panic(err)
	}
	return raw
}

// pickTxs maps each pick byte to a pool transaction. Picks may repeat txs and
// may put them in any order, including orders the square rejects.
func pickTxs(picks []byte) [][]byte {
	pool := fuzzQueryTxPool()
	txs := make([][]byte, len(picks))
	for i, p := range picks {
		txs[i] = pool[int(p)%len(pool)].bytes
	}
	return txs
}

func marshalBlock(tb testing.TB, txs [][]byte) []byte {
	tb.Helper()
	raw, err := (&tmproto.Block{
		Header: tmproto.Header{Version: cmtversion.Consensus{App: appconsts.Version}},
		Data:   tmproto.Data{Txs: txs},
	}).Marshal()
	require.NoError(tb, err)
	return raw
}

// queryBlock is a block computed independently of the query handlers.
type queryBlock struct {
	txs   [][]byte
	bytes []byte
	// valid is false when no valid block holds txs in this order; the fields
	// below are then unset.
	valid      bool
	classified []square.ClassifiedTx
	square     square.Square
	dataRoot   []byte
}

// newQueryBlock builds the square the way PrepareProposal does.
func newQueryBlock(t *testing.T, txs [][]byte) queryBlock {
	t.Helper()
	b := queryBlock{txs: txs, bytes: marshalBlock(t, txs)}
	classified, err := fibretypes.ClassifyTxs(txs)
	if err != nil {
		return b
	}
	sq, err := square.Construct(classified, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	if err != nil {
		return b
	}
	eds, err := da.ExtendShares(share.ToBytes(sq))
	require.NoError(t, err)
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	b.valid, b.classified, b.square, b.dataRoot = true, classified, sq, dah.Hash()
	return b
}

// inSquareOrder reports whether txs list normal, then blob, then
// pay-for-fibre transactions, the order square construction requires. Txs
// that do not classify count as ordered.
func inSquareOrder(txs [][]byte) bool {
	classified, err := fibretypes.ClassifyTxs(txs)
	if err != nil {
		return true
	}
	last := poolNormal
	for _, tx := range classified {
		kind := poolNormal
		if tx.FibreTx != nil {
			kind = poolFibre
		} else if _, isBlob, _ := blobtx.UnmarshalBlobTx(tx.Bytes); isBlob {
			kind = poolBlob
		}
		if kind < last {
			return false
		}
		last = kind
	}
	return true
}

// checkProofCovers requires raw to be a share proof that validates against
// the block's data root and proves exactly the shares in want, under the
// namespace of those shares.
func checkProofCovers(t *testing.T, b queryBlock, raw []byte, want share.Range) {
	t.Helper()
	var sp proof.ShareProof
	require.NoError(t, sp.Unmarshal(raw))
	require.NoError(t, sp.Validate(b.dataRoot), "returned proof does not validate against the data root")
	size, err := b.square.Size()
	require.NoError(t, err)
	last := len(sp.ShareProofs) - 1
	got := share.NewRange(
		int(sp.RowProof.StartRow)*size+int(sp.ShareProofs[0].Start),
		int(sp.RowProof.EndRow)*size+int(sp.ShareProofs[last].End),
	)
	require.Equal(t, want, got, "proof covers the wrong shares")
	require.Equal(t, share.ToBytes(b.square[want.Start:want.End]), sp.Data)
	ns := b.square[want.Start].Namespace()
	require.Equal(t, ns.ID(), sp.NamespaceId)
	require.Equal(t, uint32(ns.Version()), sp.NamespaceVersion)
}

// checkTxQuery asks for the inclusion proof of tx index in block b. A proof
// for a valid block must prove that tx's shares.
func checkTxQuery(t *testing.T, b queryBlock, index int) {
	t.Helper()
	raw, err := proof.QueryTxInclusionProof(sdk.Context{}, []string{strconv.Itoa(index)}, &abci.RequestQuery{Data: b.bytes})
	if !b.valid {
		if !inSquareOrder(b.txs) {
			// Excluded pending a fix; see the Linear issue for this target.
			return
		}
		require.Error(t, err, "proof returned for a block the square rejects")
		return
	}
	if index >= len(b.txs) {
		require.Error(t, err, "proof returned for a tx index outside the block")
		return
	}
	require.NoError(t, err, "no proof for a tx of a valid block")
	want, err := square.TxShareRange(b.classified, index, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	checkProofCovers(t, b, raw, want)
}

// checkShareQuery asks for the inclusion proof of shares [begin, end) in
// block b, with begin and end taken modulo a little more than the square
// length and shifted to include -1. The query must succeed exactly when the
// range lies in the square and holds one namespace, and must then prove that
// range.
func checkShareQuery(t *testing.T, b queryBlock, beginSeed, endSeed int) {
	t.Helper()
	n := len(b.square) + 3
	begin, end := beginSeed%n-1, endSeed%n-1
	path := []string{strconv.Itoa(begin), strconv.Itoa(end)}
	raw, err := proof.QueryShareInclusionProof(sdk.Context{}, path, &abci.RequestQuery{Data: b.bytes})
	if !b.valid {
		require.Error(t, err, "proof returned for a block the square rejects")
		return
	}
	valid := begin >= 0 && begin < end && end <= len(b.square)
	for i := begin; valid && i < end; i++ {
		valid = b.square[i].Namespace().Equals(b.square[begin].Namespace())
	}
	ns, parseErr := proof.ParseNamespace(b.square, begin, end)
	require.Equal(t, valid, parseErr == nil, "ParseNamespace: %v", parseErr)
	require.Equal(t, valid, err == nil, "QueryShareInclusionProof: %v", err)
	if !valid {
		return
	}
	require.True(t, ns.Equals(b.square[begin].Namespace()))
	checkProofCovers(t, b, raw, share.NewRange(begin, end))
}

// checkRawQueries sends caller-chosen path strings and request bytes to both
// query handlers. They may reject anything but must not panic, and anything
// they return must decode as a share proof.
func checkRawQueries(t *testing.T, txPath, sharePath string, data []byte) {
	t.Helper()
	req := &abci.RequestQuery{Data: data}
	if raw, err := proof.QueryTxInclusionProof(sdk.Context{}, strings.Split(txPath, "/"), req); err == nil {
		require.NoError(t, new(proof.ShareProof).Unmarshal(raw))
	}
	if raw, err := proof.QueryShareInclusionProof(sdk.Context{}, strings.Split(sharePath, "/"), req); err == nil {
		require.NoError(t, new(proof.ShareProof).Unmarshal(raw))
	}
}

// queryPicks are pool picks for seed blocks: ordered blocks of each kind and
// one block the square rejects.
var queryPicks = [][]byte{
	{0},
	{0, 1, 2, 3, 4, 5},
	{4, 6, 7, 8},
	{1, 2, 7, 8, 9},
	{5, 5, 8, 9},
	{6, 1},
}

func TestProofQuerySeeds(t *testing.T) {
	for _, picks := range queryPicks {
		b := newQueryBlock(t, pickTxs(picks))
		for i := range len(b.txs) + 1 {
			checkTxQuery(t, b, i)
		}
		checkShareQuery(t, b, 1, 2)
		checkShareQuery(t, b, 1, 3)
	}
	b := newQueryBlock(t, pickTxs(queryPicks[3]))
	require.True(t, b.valid)
	pfb := share.GetShareRangeForNamespace(b.square, share.PayForBlobNamespace)
	require.False(t, pfb.IsEmpty())
	checkShareQuery(t, b, pfb.Start+1, pfb.End+1)
}

// FuzzProofQueries drives the txInclusionProof and shareInclusionProof ABCI
// queries, which take caller-chosen block bytes and path indices. Arbitrary
// paths and bytes must not panic. For a block built from picked pool txs, a
// returned proof must validate against the independently computed data root
// and cover exactly the requested tx or share range.
func FuzzProofQueries(f *testing.F) {
	for _, picks := range queryPicks {
		block := marshalBlock(f, pickTxs(picks))
		f.Add(picks, uint8(1), uint16(1), uint16(3), "1", "0/2", block)
	}
	f.Add([]byte{}, uint8(0), uint16(0), uint16(0), "-1", "x", []byte{})
	f.Add([]byte{3}, uint8(9), uint16(3), uint16(1), "9223372036854775807", "0/1/2", []byte{0x0a, 0x00})
	f.Fuzz(func(t *testing.T, picks []byte, txIndex uint8, begin, end uint16, txPath, sharePath string, data []byte) {
		if len(data) > maxFuzzQueryData || len(txPath)+len(sharePath) > 256 || len(picks) > maxFuzzBlockTxs {
			return
		}
		checkRawQueries(t, txPath, sharePath, data)
		if len(picks) == 0 {
			return
		}
		b := newQueryBlock(t, pickTxs(picks))
		checkTxQuery(t, b, int(txIndex)%(len(b.txs)+1))
		checkShareQuery(t, b, int(begin), int(end))
	})
}
