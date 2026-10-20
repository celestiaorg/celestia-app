package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestProposalPayForFibreAnteMatchesFinalizeBlock pins the flat PFF walk to
// FinalizeBlock's chain (error, gas, sequence, balance). Its cases reach every
// decorator but ExtensionOptions (never decodes) and validator signatures (unchecked).
func TestProposalPayForFibreAnteMatchesFinalizeBlock(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
	owner := testfactory.GetAddress(kr, accounts[0])
	seedFibreEscrow(t, testApp, owner, 1_000_000)

	staleSigner := newSigner(0)
	require.NoError(t, staleSigner.SetSequence(accounts[0], infos[0].Sequence+5))

	setupCtx := testApp.NewUncachedContext(false, cmtproto.Header{ChainID: testutil.ChainID, Height: testApp.LastBlockHeight()})
	maxMemo := testApp.AccountKeeper.GetParams(setupCtx).MaxMemoCharacters

	cases := map[string][]byte{
		"valid":                 newSignedPayForFibreTx(t, newSigner(0), accounts[0], true),
		"wrong sequence":        newSignedPayForFibreTx(t, staleSigner, accounts[0], true),
		"gas limit 10k":         newSignedPayForFibreTxWithOpts(t, newSigner(0), accounts[0], true, user.SetGasLimit(10_000), user.SetFee(4_000)),
		"gas limit 40k":         newSignedPayForFibreTxWithOpts(t, newSigner(0), accounts[0], true, user.SetGasLimit(40_000), user.SetFee(4_000)),
		"fee above the balance": newSignedPayForFibreTxWithOpts(t, newSigner(0), accounts[0], true, user.SetGasLimit(1_000_000), user.SetFee(1<<62)),
		"memo too long":         newSignedPayForFibreTxWithOpts(t, newSigner(0), accounts[0], true, user.SetGasLimit(1_000_000), user.SetFee(4_000), user.SetMemo(strings.Repeat("m", int(maxMemo)+1))),
		"timeout height passed": newSignedPayForFibreTxWithOpts(t, newSigner(0), accounts[0], true, user.SetGasLimit(1_000_000), user.SetFee(4_000), user.SetTimeoutHeight(1)),
		"no signatures":         unsignedPayForFibreTx(t, enc.TxConfig, newSignedPayForFibreTx(t, newSigner(0), accounts[0], true)),
	}

	run := func(txBytes []byte, mode sdk.ExecMode) string {
		tx, err := enc.TxConfig.TxDecoder()(txBytes)
		require.NoError(t, err)
		ctx, _ := testApp.NewUncachedContext(false, cmtproto.Header{
			ChainID: testutil.ChainID,
			Height:  testApp.LastBlockHeight() + 1,
			Time:    time.Now(),
		}).WithExecMode(mode).WithTxBytes(txBytes).CacheContext()
		newCtx, err := testApp.AnteHandler()(ctx, tx, false)
		account := testApp.AccountKeeper.GetAccount(ctx, owner)
		balance := testApp.BankKeeper.GetBalance(ctx, owner, appconsts.BondDenom)
		return fmt.Sprintf("err=%v gas=%d sequence=%d balance=%s",
			err, newCtx.GasMeter().GasConsumed(), account.GetSequence(), balance)
	}

	for name, txBytes := range cases {
		t.Run(name, func(t *testing.T) {
			// ProcessProposal first, so it verifies signatures itself rather
			// than reading FinalizeBlock's cache entries.
			proposal := run(txBytes, sdk.ExecModeProcessProposal)
			require.Equal(t, run(txBytes, sdk.ExecModeFinalize), proposal)
			t.Log(proposal)
		})
	}
}

// unsignedPayForFibreTx re-encodes the PayForFibre in signedTx with the same
// gas and fee but no signatures.
func unsignedPayForFibreTx(t *testing.T, txConfig sdkclient.TxConfig, signedTx []byte) []byte {
	t.Helper()
	decoded, err := txConfig.TxDecoder()(signedTx)
	require.NoError(t, err)
	feeTx := decoded.(sdk.FeeTx)
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(decoded.GetMsgs()...))
	builder.SetGasLimit(feeTx.GetGas())
	builder.SetFeeAmount(feeTx.GetFee())
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}
