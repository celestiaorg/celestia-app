package fibrefactory_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/fibrefactory"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const validators = 100

// TestHistoricalInfoAtPromiseHeight asserts the promise height is readable, so
// certificates can name it. The plan assumed this rather than checking it.
func TestHistoricalInfoAtPromiseHeight(t *testing.T) {
	fixture := fibrefactory.NewFixture(t, 1, validators)
	ctx := fixture.App.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  fixture.App.LastBlockHeight(),
	})
	info, err := fixture.App.StakingKeeper.GetHistoricalInfo(ctx, fixture.PromiseHeight)
	require.NoError(t, err)
	require.Len(t, info.Valset, validators)
}

// TestQuorumSize pins the quorum the keeper actually demands. With equal voting
// power the threshold is two thirds of the total, which 66 of 100 validators do
// not reach.
func TestQuorumSize(t *testing.T) {
	fixture := fibrefactory.NewFixture(t, 1, validators)
	require.Equal(t, 67, fixture.Quorum(t))
}

// TestCheckTxAcceptsQuorumCertificate is the correctness gate for every
// benchmark built on this factory: the certificates must be ones the real
// keeper accepts, which proves the signatures are positional over the set
// ordering staking imposes, not over genesis order.
func TestCheckTxAcceptsQuorumCertificate(t *testing.T) {
	const count = 4
	fixture := fibrefactory.NewFixture(t, count, validators)
	txs := fixture.Txs(t, fixture.Quorum(t))
	require.Len(t, txs, count)

	for _, rawTx := range txs {
		resp, err := fixture.App.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, uint32(0), resp.Code, "%s: %s", resp.Codespace, resp.Log)
	}
}

// TestCheckTxRejectsSubQuorumCertificate shows the accepting case above is not
// passing on some path that ignores the certificate.
func TestCheckTxRejectsSubQuorumCertificate(t *testing.T) {
	fixture := fibrefactory.NewFixture(t, 1, validators)
	msg := fibrefactory.NewMsgPayForFibreWithQuorum(t, fixture.Signer, fixture.Accounts[0],
		fixture.PromiseHeight, fixture.BlockTime, fibrefactory.DistinctCommitment(0),
		fixture.ValPrivs, fixture.Quorum(t)-1)
	rawTx, _, err := fixture.Signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1_000_000), user.SetFee(4_000))
	require.NoError(t, err)

	resp, err := fixture.App.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.NotEqual(t, uint32(0), resp.Code, "a sub-quorum certificate must not be admitted")
}

// TestCertificateSize records what a realistic PayForFibre costs on the wire,
// the number the block-capacity model is built on.
func TestCertificateSize(t *testing.T) {
	fixture := fibrefactory.NewFixture(t, 1, validators)
	txs := fixture.Txs(t, fixture.Quorum(t))
	t.Logf("PayForFibre tx with a %d-validator certificate: %d bytes", validators, len(txs[0]))
	require.NotEmpty(t, txs[0])
}

// TestNewAppAcceptsTheSameTxs is the correctness gate for the FinalizeBlock and
// Commit benchmarks: those mutate committed state, so they rebuild the app each
// measurement, and the certificates built once must still settle against it.
func TestNewAppAcceptsTheSameTxs(t *testing.T) {
	fixture := fibrefactory.NewFixture(t, 4, validators)
	txs := fixture.Txs(t, fixture.Quorum(t))

	rebuilt := fixture.NewApp(t)
	require.Equal(t, fixture.PromiseHeight, rebuilt.LastBlockHeight())

	resp, err := rebuilt.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: rebuilt.LastBlockHeight() + 1,
		Time:   fixture.BlockTime,
		Txs:    txs,
		Hash:   rebuilt.LastCommitID().Hash,
	})
	require.NoError(t, err)
	for i, result := range resp.TxResults {
		require.Equal(t, uint32(0), result.Code, "tx %d: %s: %s", i, result.Codespace, result.Log)
	}
}

// TestDecodedGasLimitMatchesSignedTx checks the declared gas is read correctly
// from a real signed transaction. The pre-verification pass skips transactions
// that cannot pay for their own signature checks, and it reads that limit by
// scanning the auth info rather than decoding it.
func TestDecodedGasLimitMatchesSignedTx(t *testing.T) {
	const gasLimit = 1_000_000

	fixture := fibrefactory.NewFixture(t, 1, validators)
	txs := fixture.Txs(t, fixture.Quorum(t))

	decoded := keeper.DecodePayForFibre(txs[0])
	require.NotNil(t, decoded)
	require.Equal(t, uint64(gasLimit), decoded.GasLimit)
}
