package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestProposalPayForFibreAnteMatchesFinalizeBlock pins the flat ante walk a
// single-message PayForFibre takes in ProcessProposal to the full chain
// FinalizeBlock runs: same error, same gas, same sequence and balance after.
// A decorator marked inert for that walk that starts doing work for a PFF, or
// a decorator that starts doing work after next, makes the two differ.
// Validator signatures are left valid: FinalizeBlock skips verifying them by
// design, so a forged one is the one input the two phases may treat apart.
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

	cases := map[string][]byte{
		"valid":                 newSignedPayForFibreTx(t, newSigner(0), accounts[0], true),
		"wrong sequence":        newSignedPayForFibreTx(t, staleSigner, accounts[0], true),
		"gas limit 10k":         newSignedPayForFibreTxWithOpts(t, newSigner(0), accounts[0], true, user.SetGasLimit(10_000), user.SetFee(4_000)),
		"gas limit 40k":         newSignedPayForFibreTxWithOpts(t, newSigner(0), accounts[0], true, user.SetGasLimit(40_000), user.SetFee(4_000)),
		"fee above the balance": newSignedPayForFibreTxWithOpts(t, newSigner(0), accounts[0], true, user.SetGasLimit(1_000_000), user.SetFee(1<<62)),
	}

	run := func(txBytes []byte, mode sdk.ExecMode) string {
		tx, err := enc.TxConfig.TxDecoder()(txBytes)
		require.NoError(t, err)
		ctx, _ := testApp.NewUncachedContext(false, cmtproto.Header{
			ChainID: testutil.ChainID,
			Height:  testApp.LastBlockHeight() + 1,
			Time:    time.Now(),
		}).WithExecMode(mode).CacheContext()
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
