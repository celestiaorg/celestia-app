package ante_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v11/app"
	appante "github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibreante "github.com/celestiaorg/celestia-app/v11/x/fibre/ante"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// overlapFibreKeeper stands in for the fibre keeper so each case picks the
// certificate and promise verdicts, and how long the pre-pass takes.
type overlapFibreKeeper struct {
	cache        *sigcache.Cache
	certErr      error
	statefulErr  error
	prepassDelay time.Duration
}

func (k overlapFibreKeeper) ValidatePayForFibreSignatures(sdk.Context, *fibretypes.MsgPayForFibre) error {
	return k.certErr
}

func (overlapFibreKeeper) ValidateValidatorSetAvailable(sdk.Context, int64) error { return nil }

func (k overlapFibreKeeper) PreverifyPayForFibreMsg(_ sdk.Context, _ *fibretypes.MsgPayForFibre, certKey sigcache.Key) {
	time.Sleep(k.prepassDelay)
	if k.certErr == nil {
		k.cache.Add(certKey)
	}
}

func (k overlapFibreKeeper) ValidatePaymentPromiseStateful(sdk.Context, *fibretypes.PaymentPromise) (time.Time, error) {
	return time.Time{}, k.statefulErr
}

// anteOutcome is everything runTx reads back from an ante pass.
type anteOutcome struct {
	err      string
	code     uint32
	gasUsed  uint64
	gasLimit uint64
	panicked string
	sequence uint64
}

// TestTxSigOverlapMatchesInlineVerification runs the same txs through the ante
// chain with and without TxSigOverlapDecorator and requires identical outcomes.
func TestTxSigOverlapMatchesInlineVerification(t *testing.T) {
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	baseCtx := testApp.NewUncachedContext(true, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight() + 1,
		Time:    time.Now(),
	}).WithChainID(testutil.ChainID)
	addr := testfactory.GetAddress(kr, accounts[0])
	acc := testApp.AccountKeeper.GetAccount(baseCtx, addr)
	require.NotNil(t, acc)

	record, err := kr.Key(accounts[0])
	require.NoError(t, err)
	pubKey, err := record.GetPubKey()
	require.NoError(t, err)

	fibreGas := fibretypes.EstimateGasForPayForFibreSignatureVerification(1)
	// gasAtFibre is what a PFF has used once its signature decorator passes, so a
	// limit just above it runs out of gas in the fibre gas decorator, after it.
	gasAtFibre := func(t *testing.T, tx sdk.Tx) uint64 {
		handler := sdk.ChainAnteDecorators(
			ante.NewSetUpContextDecorator(),
			ante.NewSetPubKeyDecorator(testApp.AccountKeeper),
			ante.NewSigGasConsumeDecorator(testApp.AccountKeeper, ante.DefaultSigVerificationGasConsumer),
			appante.NewCachedSigVerificationDecorator(testApp.AccountKeeper, enc.TxConfig.SignModeHandler(), sigcache.New(16)),
		)
		ctx, _ := baseCtx.CacheContext()
		newCtx, err := handler(ctx, tx, false)
		require.NoError(t, err)
		return newCtx.GasMeter().GasConsumed()
	}
	newTx := func(t *testing.T, pff bool, accNum, gasLimit uint64) sdk.Tx {
		signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[0], accNum, acc.GetSequence()))
		require.NoError(t, err)
		var msg sdk.Msg = banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
		if pff {
			msg = blobfactory.NewMsgPayForFibre(t, pubKey.(*secp256k1.PubKey), testutil.ChainID)
		}
		txBytes, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(gasLimit), user.SetFee(2_000))
		require.NoError(t, err)
		tx, err := enc.TxConfig.TxDecoder()(txBytes)
		require.NoError(t, err)
		return tx
	}

	run := func(overlap bool, tx sdk.Tx, k overlapFibreKeeper, ctx sdk.Context, simulate bool) (out anteOutcome) {
		txCache := sigcache.New(16)
		k.cache = sigcache.New(16)
		decorators := []sdk.AnteDecorator{
			ante.NewSetUpContextDecorator(),
			ante.NewSetPubKeyDecorator(testApp.AccountKeeper),
			ante.NewSigGasConsumeDecorator(testApp.AccountKeeper, ante.DefaultSigVerificationGasConsumer),
		}
		if overlap {
			decorators = append(decorators, appante.NewTxSigOverlapDecorator())
		}
		decorators = append(decorators,
			appante.NewCachedSigVerificationDecorator(testApp.AccountKeeper, enc.TxConfig.SignModeHandler(), txCache),
			fibreante.NewFibreSignatureGasDecorator(),
			fibreante.NewFibreStatefulValidationDecorator(k),
			fibreante.NewFibreSigVerificationDecorator(k, k.cache),
			ante.NewIncrementSequenceDecorator(testApp.AccountKeeper),
		)
		handler := sdk.ChainAnteDecorators(decorators...)

		ctx, _ = ctx.CacheContext()
		defer func() {
			if r := recover(); r != nil {
				out.panicked = fmt.Sprint(r)
			}
		}()
		newCtx, err := handler(ctx, tx, simulate)
		if err != nil {
			out.err = err.Error()
			_, out.code, _ = errorsmod.ABCIInfo(err, false)
		}
		if !newCtx.IsZero() {
			out.gasUsed = newCtx.GasMeter().GasConsumed()
			out.gasLimit = newCtx.GasMeter().Limit()
		}
		if err == nil {
			// runTx drops the branch of a failed tx, so only an accepted one's writes count.
			out.sequence = testApp.AccountKeeper.GetAccount(ctx, addr).GetSequence()
		}
		return out
	}

	certErr := errors.New("validator signature validation failed")
	statefulErr := errors.New("promise already processed")
	modes := map[string]struct {
		ctx      sdk.Context
		simulate bool
	}{
		"check":    {baseCtx, false},
		"recheck":  {baseCtx.WithIsReCheckTx(true), false},
		"simulate": {baseCtx, true},
	}

	cases := 0
	for modeName, mode := range modes {
		for _, pff := range []bool{true, false} {
			for _, badSig := range []bool{false, true} {
				for _, gas := range []uint64{1_000_000, 0, fibreGas + 1} {
					for _, k := range []overlapFibreKeeper{
						{},
						{certErr: certErr},
						{statefulErr: statefulErr},
						{prepassDelay: 5 * time.Millisecond},
						{certErr: certErr, prepassDelay: 5 * time.Millisecond},
					} {
						accNum := acc.GetAccountNumber()
						if badSig {
							accNum++
						}
						name := fmt.Sprintf("%s/pff=%t/badSig=%t/gas=%d/cert=%v/stateful=%v/delay=%v",
							modeName, pff, badSig, gas, k.certErr, k.statefulErr, k.prepassDelay)
						t.Run(name, func(t *testing.T) {
							if gas == 0 {
								gas = gasAtFibre(t, newTx(t, pff, acc.GetAccountNumber(), 1_000_000)) + 1
							}
							tx := newTx(t, pff, accNum, gas)
							inline := run(false, tx, k, mode.ctx, mode.simulate)
							overlapped := run(true, tx, k, mode.ctx, mode.simulate)
							require.Equal(t, inline, overlapped)
						})
						cases++
					}
				}
			}
		}
	}
	require.Equal(t, 180, cases)
}
