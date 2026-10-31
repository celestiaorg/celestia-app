package ante_test

import (
	"context"
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
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibreante "github.com/celestiaorg/celestia-app/v11/x/fibre/ante"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
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
	// fallbacks counts sequential certificate checks.
	fallbacks *int
}

func (k overlapFibreKeeper) ValidatePayForFibreSignatures(sdk.Context, *fibretypes.MsgPayForFibre) error {
	*k.fallbacks++
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
	// fallbacks: a tx whose signature fails must not pay the sequential certificate check.
	fallbacks int
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
	newTx := func(t *testing.T, pff bool, accNum, gasLimit uint64, signMode signing.SignMode) sdk.Tx {
		var msg sdk.Msg = banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
		if pff {
			msg = blobfactory.NewMsgPayForFibre(t, pubKey.(*secp256k1.PubKey), testutil.ChainID)
		}
		builder := enc.TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msg))
		builder.SetGasLimit(gasLimit)
		builder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 2_000)))
		txf := clienttx.Factory{}.WithKeybase(kr).WithTxConfig(enc.TxConfig).WithChainID(testutil.ChainID).
			WithAccountNumber(accNum).WithSequence(acc.GetSequence()).WithSignMode(signMode)
		require.NoError(t, clienttx.Sign(context.Background(), txf, accounts[0], builder, true))
		return builder.GetTx()
	}

	run := func(overlap bool, tx sdk.Tx, k overlapFibreKeeper, ctx sdk.Context, simulate bool) (out anteOutcome) {
		txCache := sigcache.New(16)
		k.cache = sigcache.New(16)
		k.fallbacks = new(int)
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
			out.fallbacks = *k.fallbacks
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
	// Only SIGN_MODE_DIRECT goes off the ante goroutine; amino JSON has its own error wording.
	signModes := []signing.SignMode{signing.SignMode_SIGN_MODE_DIRECT, signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON}
	for modeName, mode := range modes {
		for _, signMode := range signModes {
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
							name := fmt.Sprintf("%s/%s/pff=%t/badSig=%t/gas=%d/cert=%v/stateful=%v/delay=%v",
								modeName, signMode, pff, badSig, gas, k.certErr, k.statefulErr, k.prepassDelay)
							t.Run(name, func(t *testing.T) {
								if gas == 0 {
									gas = gasAtFibre(t, newTx(t, pff, acc.GetAccountNumber(), 1_000_000, signMode)) + 1
								}
								tx := newTx(t, pff, accNum, gas, signMode)
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
	}
	require.Equal(t, 360, cases)
}
