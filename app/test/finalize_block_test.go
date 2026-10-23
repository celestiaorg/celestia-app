package app_test

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

const pffTestEscrowAmount = 10_000_000

// TestPayForFibreReplayGasParity verifies cached and uncached txs use the same gas.
func TestPayForFibreReplayGasParity(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(3)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)

	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
	sendTx, pffTxs := buildGasParityTxs(t, testApp, kr, newSigner, accounts)

	// Cache the first PFF tx through CheckTx.
	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: pffTxs[0], Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)

	finalizeResp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:   time.Now(),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
		Txs:    [][]byte{sendTx, pffTxs[0], pffTxs[1]},
	})
	require.NoError(t, err)
	require.Len(t, finalizeResp.TxResults, 3)
	for i, result := range finalizeResp.TxResults {
		require.Equal(t, abci.CodeTypeOK, result.Code, "tx %d: %s", i, result.Log)
	}
	require.Equal(t, finalizeResp.TxResults[1].GasUsed, finalizeResp.TxResults[2].GasUsed,
		"cached and uncached signature verification must consume identical gas")
}

// TestPayForFibreFinalizeBlockColdCacheWithPrunedHistory replays a committed
// PFF tx on a node whose signature cache is cold (as after state sync or a
// restart) once the promise height's historical info has been pruned. The
// result must match the live network, which settled the tx with a warm cache.
func TestPayForFibreFinalizeBlockColdCacheWithPrunedHistory(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), pffTestEscrowAmount)

	// Keep only the two most recent historical entries so the promise height
	// (1) gets pruned while still inside PaymentPromiseHeightWindow.
	setHistoricalEntries(t, testApp, 2)
	for testApp.LastBlockHeight() < 5 {
		execBlock(t, testApp, func(sdk.Context) {})
	}
	_, err := testApp.StakingKeeper.GetHistoricalInfo(uncachedContext(testApp), 1)
	require.Error(t, err, "promise height must be pruned for this test")

	// The tx never went through CheckTx or ProcessProposal on this node, so
	// the signature cache is cold.
	pffTx := newSignedPayForFibreTx(t, signer, accounts[0], true)
	resp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:   time.Now(),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
		Txs:    [][]byte{pffTx},
	})
	require.NoError(t, err)
	require.Len(t, resp.TxResults, 1)
	require.Equal(t, abci.CodeTypeOK, resp.TxResults[0].Code, resp.TxResults[0].Log)
}

func uncachedContext(testApp *app.App) sdk.Context {
	return testApp.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight(),
		Time:    time.Now(),
	})
}

func setHistoricalEntries(t *testing.T, testApp *app.App, entries uint32) {
	t.Helper()
	ctx := uncachedContext(testApp)
	params, err := testApp.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	params.HistoricalEntries = entries
	require.NoError(t, testApp.StakingKeeper.SetParams(ctx, params))
}

// TestValidatorSetAvailableAgreesWithStaking checks the fibre existence check,
// through the app's wiring, against staking's own read at every kind of height.
func TestValidatorSetAvailableAgreesWithStaking(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	setHistoricalEntries(t, testApp, 2)
	for testApp.LastBlockHeight() < 5 {
		execBlock(t, testApp, func(sdk.Context) {})
	}
	ctx := uncachedContext(testApp)
	current := testApp.LastBlockHeight()

	stored := map[int64]bool{}
	for height := int64(-1); height <= current+2; height++ {
		// A promise at height is signed by the set staking records at height-1.
		_, getErr := testApp.StakingKeeper.GetHistoricalInfo(ctx, height-1)
		availErr := testApp.FibreKeeper.ValidateValidatorSetAvailable(ctx, height)
		stored[height] = getErr == nil
		if getErr == nil {
			require.NoError(t, availErr, "height %d", height)
			continue
		}
		require.ErrorIs(t, getErr, stakingtypes.ErrNoHistoricalInfo, "height %d", height)
		require.ErrorContains(t, availErr, "failed to get historical validator set at height", "height %d", height)
		require.ErrorContains(t, availErr, getErr.Error(), "height %d", height)
	}
	// Pruned, oldest kept, current, next, future, zero and negative must all have been covered.
	// The next height is available: its signing set is the current one.
	require.Equal(t, map[int64]bool{-1: false, 0: false, 1: false, 2: false, 3: false, 4: false, 5: true, 6: true, 7: false}, stored)

	// Production reads through cachekv, so check that branch too.
	checkCtx := testApp.NewContext(true)
	require.NoError(t, testApp.FibreKeeper.ValidateValidatorSetAvailable(checkCtx, current))
	require.ErrorContains(t, testApp.FibreKeeper.ValidateValidatorSetAvailable(checkCtx, 2), "failed to get historical validator set at height 1")
}

// buildGasParityTxs returns a MsgSend tx and two same-sized MsgPayForFibre txs.
func buildGasParityTxs(t *testing.T, testApp *app.App, kr keyring.Keyring, newSigner func(int) *user.Signer, accounts []string) ([]byte, [][]byte) {
	t.Helper()
	require.Len(t, accounts, 3)

	addr0 := testfactory.GetAddress(kr, accounts[0])
	sendMsg := banktypes.NewMsgSend(addr0, addr0, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
	sendTx, _, err := newSigner(0).CreateTx([]sdk.Msg{sendMsg}, user.SetGasLimit(200_000), user.SetFee(1_000_000))
	require.NoError(t, err)

	pffTxs := make([][]byte, 2)
	for i := range pffTxs {
		account := accounts[i+1]
		seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, account), pffTestEscrowAmount)
		pffTxs[i] = newSignedPayForFibreTxWithOpts(t, newSigner(i+1), account, true,
			user.SetGasLimit(1_000_000), user.SetFee(10_000))
	}
	require.Equal(t, len(pffTxs[0]), len(pffTxs[1]), "PayForFibre txs must be identically sized for gas parity")
	return sendTx, pffTxs
}

// TestPayForFibreSigCacheRejectsMutatedCertificates checks that a cached
// certificate never lets a mutated copy of it pass ProcessProposal.
func TestPayForFibreSigCacheRejectsMutatedCertificates(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	account := accounts[0]
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, account), pffTestEscrowAmount)

	// Warm the cache with the original certificate.
	cachedTx := newSignedPayForFibreTx(t, signer, account, true)
	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: cachedTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)
	requireProcessProposalStatus(t, testApp, cachedTx, abci.ResponseProcessProposal_ACCEPT)

	signPromise := func(msg *fibretypes.MsgPayForFibre) {
		var err error
		msg.PaymentPromise.Signature, _, err = signer.Keyring().Sign(account, promiseSignBytes(t, msg), signing.SignMode_SIGN_MODE_DIRECT)
		require.NoError(t, err)
	}
	signValidators := func(msg *fibretypes.MsgPayForFibre) {
		signature, err := testutil.GenesisValidatorPrivateKey().Sign(promiseSignBytes(t, msg))
		require.NoError(t, err)
		msg.ValidatorSignatures = [][]byte{signature}
	}

	tests := []struct {
		name   string
		mutate func(*fibretypes.MsgPayForFibre)
		want   abci.ResponseProcessProposal_ProposalStatus
	}{
		{
			name: "fully re-signed changed promise is accepted",
			mutate: func(msg *fibretypes.MsgPayForFibre) {
				msg.PaymentPromise.BlobSize++
				signPromise(msg)
				signValidators(msg)
			},
			want: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:   "flipped validator signature byte",
			mutate: func(msg *fibretypes.MsgPayForFibre) { msg.ValidatorSignatures[0][0] ^= 1 },
			want:   abci.ResponseProcessProposal_REJECT,
		},
		{
			name:   "empty validator signature misses quorum",
			mutate: func(msg *fibretypes.MsgPayForFibre) { msg.ValidatorSignatures = [][]byte{{}} },
			want:   abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "trailing entry beyond the validator set",
			mutate: func(msg *fibretypes.MsgPayForFibre) {
				msg.ValidatorSignatures = append(msg.ValidatorSignatures, []byte{})
			},
			want: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "validator signature reused for a re-signed promise",
			mutate: func(msg *fibretypes.MsgPayForFibre) {
				msg.PaymentPromise.BlobSize++
				signPromise(msg)
			},
			want: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:   "flipped promise signature byte",
			mutate: func(msg *fibretypes.MsgPayForFibre) { msg.PaymentPromise.Signature[0] ^= 1 },
			want:   abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "promise height without historical validator set",
			mutate: func(msg *fibretypes.MsgPayForFibre) {
				// Inside the height window, but its signing set (historical
				// info for the proposed block) is not recorded yet.
				msg.PaymentPromise.Height = testApp.LastBlockHeight() + 2
				signPromise(msg)
				signValidators(msg)
			},
			want: abci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := payForFibreMsg(t, enc, cachedTx)
			tc.mutate(msg)
			mutatedTx, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1_000_000), user.SetFee(4_000))
			require.NoError(t, err)
			requireProcessProposalStatus(t, testApp, mutatedTx, tc.want)
		})
	}

	// Rejections must not evict or poison the cached certificate.
	requireProcessProposalStatus(t, testApp, cachedTx, abci.ResponseProcessProposal_ACCEPT)
}

// TestPayForFibreFinalizeBlockCacheParity checks that a node's signature cache
// state does not change FinalizeBlock results, gas, or app hash.
func TestPayForFibreFinalizeBlockCacheParity(t *testing.T) {
	blockTime := time.Now().Truncate(time.Second)
	modes := []struct {
		name    string
		prepare func(t *testing.T, testApp *app.App, txs [][]byte)
	}{
		{
			name: "warm cache from CheckTx and ProcessProposal",
			prepare: func(t *testing.T, testApp *app.App, txs [][]byte) {
				for _, tx := range txs {
					resp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
					require.NoError(t, err)
					require.Equal(t, abci.CodeTypeOK, resp.Code, resp.Log)
				}
				requireProcessProposalAccepts(t, testApp, txs, blockTime)
			},
		},
		{
			name: "empty cache at ProcessProposal",
			prepare: func(t *testing.T, testApp *app.App, txs [][]byte) {
				requireProcessProposalAccepts(t, testApp, txs, blockTime)
			},
		},
		{
			name:    "cold cache replay",
			prepare: func(*testing.T, *app.App, [][]byte) {},
		},
	}

	var (
		txs        [][]byte
		wantHash   []byte
		wantResult *abci.ResponseFinalizeBlock
	)
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			testApp, signer, account := newSenderApp(t)
			seedFibreEscrow(t, testApp, signer.Account(account).Address(), pffTestEscrowAmount)
			commitBlockAt(t, testApp, blockTime.Add(-time.Second))

			if txs == nil {
				txs = newPayForFibreTxPair(t, signer, account, blockTime.Add(-2*time.Second), blockTime.Add(-time.Second))
				wantHash = testApp.LastCommitID().Hash
			}
			require.Equal(t, wantHash, testApp.LastCommitID().Hash, "apps must start from identical state")

			mode.prepare(t, testApp, txs)
			resp := commitBlockAt(t, testApp, blockTime, txs...)
			require.Len(t, resp.TxResults, len(txs))
			for i, result := range resp.TxResults {
				require.Equal(t, abci.CodeTypeOK, result.Code, "tx %d: %s", i, result.Log)
			}

			if wantResult == nil {
				wantResult = resp
				return
			}
			require.Equal(t, wantResult.TxResults, resp.TxResults)
			require.Equal(t, wantResult.AppHash, resp.AppHash)
		})
	}
}

// newSenderApp returns a test app and a signer for its fixed-key genesis
// sender, so separately built apps start from identical state.
func newSenderApp(t *testing.T) (*app.App, *user.Signer, string) {
	t.Helper()
	const account = "sender"
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())

	// Same secret as the sender in testutil.GenesisStateWithSingleValidator.
	privKey := secp256k1.GenPrivKeyFromSecret([]byte("09876543210987654321098765432109"))
	require.NoError(t, kr.ImportPrivKeyHex(account, hex.EncodeToString(privKey.Key), "secp256k1"))
	acc := testutil.DirectQueryAccount(testApp, sdk.AccAddress(privKey.PubKey().Address()))
	require.NotNil(t, acc, "sender must be a genesis account")

	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID,
		user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(t, err)
	return testApp, signer, account
}

// payForFibreMsg decodes the MsgPayForFibre in txBytes.
func payForFibreMsg(t *testing.T, enc encoding.Config, txBytes []byte) *fibretypes.MsgPayForFibre {
	t.Helper()
	sdkTx, err := enc.TxConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	msgs := sdkTx.GetMsgs()
	require.Len(t, msgs, 1)
	msg, ok := msgs[0].(*fibretypes.MsgPayForFibre)
	require.True(t, ok)
	return msg
}

// promiseSignBytes returns the bytes signed by msg's promise signer and validators.
func promiseSignBytes(t *testing.T, msg *fibretypes.MsgPayForFibre) []byte {
	t.Helper()
	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&msg.PaymentPromise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	return signBytes
}

func requireProcessProposalStatus(t *testing.T, testApp *app.App, tx []byte, want abci.ResponseProcessProposal_ProposalStatus) {
	t.Helper()
	resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, [][]byte{tx}))
	require.NoError(t, err)
	require.Equal(t, want, resp.Status)
}

func requireProcessProposalAccepts(t *testing.T, testApp *app.App, txs [][]byte, blockTime time.Time) {
	t.Helper()
	req := processProposalRequest(t, testApp, txs)
	req.Time = blockTime
	resp, err := testApp.ProcessProposal(req)
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
}
