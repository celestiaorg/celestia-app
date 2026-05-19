package app_test

import (
	"crypto/ecdsa"
	"encoding/hex"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/pkg/tx/eip712"
	testutil "github.com/celestiaorg/celestia-app/v9/test/util"
	"github.com/celestiaorg/celestia-app/v9/test/util/genesis"
	ethidentitykeeper "github.com/celestiaorg/celestia-app/v9/x/ethidentity/keeper"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestEIP712TxABCIPaths(t *testing.T) {
	t.Run("CheckTx accepts", func(t *testing.T) {
		testApp, txBytes := setupEIP712AppAndTx(t, false)

		resp, err := testApp.CheckTx(&abci.RequestCheckTx{
			Tx:   txBytes,
			Type: abci.CheckTxType_New,
		})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code, resp.Log)
	})

	t.Run("PrepareProposal and ProcessProposal accept", func(t *testing.T) {
		testApp, txBytes := setupEIP712AppAndTx(t, false)
		height := testApp.LastBlockHeight() + 1
		blockTime := time.Now()

		prepareResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    [][]byte{txBytes},
			Height: height,
			Time:   blockTime,
		})
		require.NoError(t, err)
		require.Equal(t, [][]byte{txBytes}, prepareResp.Txs)

		processResp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
			Txs:          prepareResp.Txs,
			Height:       height,
			Time:         blockTime,
			DataRootHash: prepareResp.DataRootHash,
			SquareSize:   prepareResp.SquareSize,
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processResp.Status)
	})

	t.Run("FinalizeBlock accepts", func(t *testing.T) {
		testApp, txBytes := setupEIP712AppAndTx(t, false)

		resp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Time:   time.Now(),
			Txs:    [][]byte{txBytes},
			Height: testApp.LastBlockHeight() + 1,
			Hash:   testApp.LastCommitID().Hash,
		})
		require.NoError(t, err)
		require.Len(t, resp.TxResults, 1)
		require.Equal(t, uint32(0), resp.TxResults[0].Code, resp.TxResults[0].Log)
		_, err = testApp.Commit()
		require.NoError(t, err)

		priv := eip712TestPrivKey(t)
		pubKey := &secp256k1.PubKey{Key: gethcrypto.CompressPubkey(&priv.PublicKey)}
		ethAddr, err := ethidentitykeeper.EthereumAddressFromPubKey(pubKey)
		require.NoError(t, err)

		ctx := testApp.NewUncachedContext(false, tmproto.Header{ChainID: testutil.ChainID})
		resolved, found := testApp.EthIdentityKeeper.Resolve(ctx, ethAddr)
		require.True(t, found)
		require.Equal(t, sdk.AccAddress(pubKey.Address()), resolved)
	})
}

func TestMalformedEIP712TxRejectedAcrossABCIPaths(t *testing.T) {
	t.Run("CheckTx rejects", func(t *testing.T) {
		testApp, txBytes := setupEIP712AppAndTx(t, true)

		resp, err := testApp.CheckTx(&abci.RequestCheckTx{
			Tx:   txBytes,
			Type: abci.CheckTxType_New,
		})
		if resp.Code == abci.CodeTypeOK {
			require.NoError(t, err)
		}
		require.NotEqual(t, abci.CodeTypeOK, resp.Code)
	})

	t.Run("PrepareProposal filters and ProcessProposal rejects", func(t *testing.T) {
		testApp, txBytes := setupEIP712AppAndTx(t, true)
		height := testApp.LastBlockHeight() + 1
		blockTime := time.Now()

		prepareResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    [][]byte{txBytes},
			Height: height,
			Time:   blockTime,
		})
		require.NoError(t, err)
		require.Empty(t, prepareResp.Txs)

		processResp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
			Txs:          [][]byte{txBytes},
			Height:       height,
			Time:         blockTime,
			DataRootHash: calculateNewDataHash(t, [][]byte{txBytes}),
			SquareSize:   1,
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, processResp.Status)
	})

	t.Run("FinalizeBlock rejects", func(t *testing.T) {
		testApp, txBytes := setupEIP712AppAndTx(t, true)

		resp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Time:   time.Now(),
			Txs:    [][]byte{txBytes},
			Height: testApp.LastBlockHeight() + 1,
			Hash:   testApp.LastCommitID().Hash,
		})
		require.NoError(t, err)
		require.Len(t, resp.TxResults, 1)
		require.NotEqual(t, uint32(0), resp.TxResults[0].Code)
	})
}

func setupEIP712AppAndTx(t *testing.T, tamperSignature bool) (*app.App, []byte) {
	t.Helper()

	priv := eip712TestPrivKey(t)
	pubKey := &secp256k1.PubKey{Key: gethcrypto.CompressPubkey(&priv.PublicKey)}
	signer := sdk.AccAddress(pubKey.Address())

	testApp := testutil.NewTestApp()
	genesisState, valSet, _ := testutil.GenesisStateWithSingleValidator(testApp)
	initialBalance := sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1000000000))
	genesisState = genesis.FundAccounts(
		testApp.AppCodec(),
		[]sdk.AccAddress{signer},
		initialBalance,
	)(genesisState)
	var bankGenesis banktypes.GenesisState
	testApp.AppCodec().MustUnmarshalJSON(genesisState[banktypes.ModuleName], &bankGenesis)
	bankGenesis.Supply = bankGenesis.Supply.Add(initialBalance)
	genesisState[banktypes.ModuleName] = testApp.AppCodec().MustMarshalJSON(&bankGenesis)
	testApp = testutil.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), genesisState)
	_, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:               testutil.GenesisTime,
		Height:             testApp.LastBlockHeight() + 1,
		Hash:               testApp.LastCommitID().Hash,
		NextValidatorsHash: valSet.Hash(),
	})
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.NoError(t, err)

	ctx := testApp.NewUncachedContext(false, tmproto.Header{ChainID: testutil.ChainID})
	acc := testApp.AccountKeeper.GetAccount(ctx, signer)
	require.NotNil(t, acc)

	txBuilder := testApp.GetTxConfig().NewTxBuilder()
	recipient := sdk.AccAddress([]byte("eip712-recipient-addr"))
	msg := banktypes.NewMsgSend(
		signer,
		recipient,
		sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)),
	)
	require.NoError(t, txBuilder.SetMsgs(msg))
	txBuilder.SetGasLimit(200000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1000)))
	ext, err := eip712.NewExtensionOptions(eip712.SchemaVersion, 12345)
	require.NoError(t, err)
	extBuilder, ok := txBuilder.(authtx.ExtensionOptionsTxBuilder)
	require.True(t, ok)
	extBuilder.SetExtensionOptions(ext)

	emptySig := signingtypes.SignatureV2{
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_EIP_712},
		Sequence: acc.GetSequence(),
	}
	require.NoError(t, txBuilder.SetSignatures(emptySig))

	txData := txBuilder.GetTx().(authsigning.V2AdaptableTx).GetSigningTxData()
	signerData := txsigning.SignerData{
		Address:       signer.String(),
		ChainID:       testutil.ChainID,
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
	}
	digest, err := eip712.Digest(signerData, txData)
	require.NoError(t, err)
	ethSig, err := gethcrypto.Sign(digest[:], priv)
	require.NoError(t, err)
	if tamperSignature {
		ethSig[len(ethSig)-1] ^= 1
	}
	sig := signingtypes.SignatureV2{
		Data: &signingtypes.SingleSignatureData{
			SignMode:  signingtypes.SignMode_SIGN_MODE_EIP_712,
			Signature: ethSig,
		},
		Sequence: acc.GetSequence(),
	}
	require.NoError(t, txBuilder.SetSignatures(sig))

	txBytes, err := testApp.GetTxConfig().TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)
	if !tamperSignature {
		require.Equal(t, "0ac4010a8f010a1c2f636f736d6f732e62616e6b2e763162657461312e4d736753656e64126f0a2f63656c6573746961317377766132647a37707239373361716d71796e346a657366387868753276647a666c76707678123163656c657374696131763435687164653378676b6879657472643963786a65747777736b6b7a65727977676d6d397837361a090a0475746961120131fa3f2f0a262f63656c65737469612e74782e76312e457874656e73696f6e4f7074696f6e734549503731321205080110b960121d0a0712050a0308c80512120a0c0a047574696112043130303010c09a0c1a41e342b963e549e7bf3915da3091d5d71f4f0e877d96a4348a93fa5dae61740a223f65cb39a27f7f5fec426238a492c546be71a03840d473c5cfac084893ee676301", hex.EncodeToString(txBytes))
	}
	return testApp, txBytes
}

func eip712TestPrivKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	privBytes, err := hex.DecodeString("4c0883a69102937d6231471b5dbb6204fe512961708279b727a63ca9b9a4b4f3")
	require.NoError(t, err)
	priv, err := gethcrypto.ToECDSA(privBytes)
	require.NoError(t, err)
	return priv
}
