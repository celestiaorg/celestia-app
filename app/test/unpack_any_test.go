package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
)

// TestUnpackAny tests executing a transaction with a MsgExec containing 176 MsgTransfer messages
func TestUnpackAny(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping TestUnpackAny in short mode")
	}

	// Create a test app with defaults
	genesisAccounts := testfactory.GenerateAccounts(1)
	testApp, keyring := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), genesisAccounts...)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// Get a validator account to use as our test account
	records, err := keyring.List()
	require.NoError(t, err)
	require.NotEmpty(t, records, "No records found in keyring")

	record := records[0]
	pubKey, err := record.GetPubKey()
	require.NoError(t, err)

	validatorAddr := sdk.AccAddress(pubKey.Address())
	t.Logf("Using validator address: %s", validatorAddr.String())

	// Get the correct chain ID from the app
	chainID := testApp.GetChainID()
	t.Logf("Using chain ID: %s", chainID)

	// Get the account number and sequence
	ctx := testApp.NewContext(false, tmproto.Header{})
	accNum := uint64(0)
	accSeq := uint64(0)

	// Query account info to get the correct account number
	account := testApp.AccountKeeper.GetAccount(ctx, validatorAddr)
	if account != nil {
		accNum = account.GetAccountNumber()
		accSeq = account.GetSequence()
	} else {
		// If account doesn't exist, create it
		account = authtypes.NewBaseAccount(validatorAddr, pubKey, accNum, accSeq)
		testApp.AccountKeeper.SetAccount(ctx, account)
	}
	t.Logf("Account number: %d, sequence: %d", accNum, accSeq)

	// Create direct transaction with MsgSend messages
	msgs := make([]sdk.Msg, 10)
	for i := 0; i < 10; i++ {
		// Create a MsgSend for each of the 10 messages
		msgSend := &banktypes.MsgSend{
			FromAddress: validatorAddr.String(),
			ToAddress:   validatorAddr.String(), // Send to self for simplicity
			Amount:      sdk.NewCoins(sdk.NewCoin("utia", sdk.NewInt(1000))),
		}
		msgs[i] = msgSend
	}

	// Instead of MsgExec, let's use direct transactions first
	txBuilder := encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msgs...))
	txBuilder.SetGasLimit(2000000) // Higher gas limit for multiple messages
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("utia", sdk.NewInt(100000))))
	txBuilder.SetMemo("Test multiple messages")

	// Sign with the validator's key
	signerData := authsigning.SignerData{
		ChainID:       chainID,
		AccountNumber: accNum,
		Sequence:      accSeq,
	}

	// Create and add an empty signature first (required by the SDK)
	signMode := signing.SignMode_SIGN_MODE_DIRECT
	sig := signing.SignatureV2{
		PubKey: pubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signMode,
			Signature: nil,
		},
		Sequence: accSeq,
	}
	err = txBuilder.SetSignatures(sig)
	require.NoError(t, err)

	// Get the sign bytes
	signBytes, err := encCfg.TxConfig.SignModeHandler().GetSignBytes(
		signMode,
		signerData,
		txBuilder.GetTx(),
	)
	require.NoError(t, err)

	// Generate the signature
	sigBytes, _, err := keyring.Sign(record.Name, signBytes)
	require.NoError(t, err)

	// Update the signature with the actual signature bytes
	sig = signing.SignatureV2{
		PubKey: pubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signMode,
			Signature: sigBytes,
		},
		Sequence: accSeq,
	}
	err = txBuilder.SetSignatures(sig)
	require.NoError(t, err)

	// Get the transaction bytes
	authTx := txBuilder.GetTx()
	txBytes, err := encCfg.TxConfig.TxEncoder()(authTx)
	require.NoError(t, err)

	// Deliver the transaction
	header := tmproto.Header{
		Version: tmversion.Consensus{
			App: appconsts.LatestVersion,
		},
		ChainID: chainID,
		Height:  testApp.LastBlockHeight() + 1,
		Time:    time.Now(),
	}

	testApp.BeginBlock(abci.RequestBeginBlock{
		Header: header,
	})

	// First check the account balance before executing
	coin := testApp.BankKeeper.GetBalance(ctx, validatorAddr, "utia")
	t.Logf("Initial balance: %s", coin.String())

	// Add funds to the account so it can pay fees and send coins
	err = testApp.BankKeeper.MintCoins(ctx, "mint", sdk.NewCoins(sdk.NewCoin("utia", sdk.NewInt(10000000))))
	require.NoError(t, err)

	err = testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", validatorAddr, sdk.NewCoins(sdk.NewCoin("utia", sdk.NewInt(10000000))))
	require.NoError(t, err)

	coin = testApp.BankKeeper.GetBalance(ctx, validatorAddr, "utia")
	t.Logf("Balance after minting: %s", coin.String())

	deliverResp := testApp.DeliverTx(abci.RequestDeliverTx{Tx: txBytes})
	t.Logf("Transaction result: code=%d, log=%s", deliverResp.Code, deliverResp.Log)

	// Now we assert that the transaction was successful
	assert.Equal(t, abci.CodeTypeOK, deliverResp.Code, "Transaction failed: %s", deliverResp.Log)

	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})
	testApp.Commit()

	t.Logf("Successfully executed a transaction with 10 messages")

	// Now that we've proven we can execute a transaction, let's create another one with MsgExec
	// This will still fail due to lack of authorization, which is expected and we won't assert success

	// Increment sequence for next transaction
	accSeq++

	// Create a MsgExec
	msgExec := &authz.MsgExec{
		Grantee: validatorAddr.String(),
		Msgs:    make([]*codectypes.Any, 0, 176),
	}

	// Pack 176 messages into Any types
	for i := 0; i < 176; i++ {
		// Create a MsgSend for each message
		msgSend := &banktypes.MsgSend{
			FromAddress: validatorAddr.String(),
			ToAddress:   validatorAddr.String(), // Send to self for simplicity
			Amount:      sdk.NewCoins(sdk.NewCoin("utia", sdk.NewInt(1000))),
		}
		anyMsg, err := codectypes.NewAnyWithValue(msgSend)
		require.NoError(t, err)
		msgExec.Msgs = append(msgExec.Msgs, anyMsg)
	}

	t.Logf("Created MsgExec with %d messages", len(msgExec.Msgs))

	// Create and sign the MsgExec transaction
	txBuilder = encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msgExec))
	txBuilder.SetGasLimit(2000000) // Higher gas limit for multiple messages
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("utia", sdk.NewInt(100000))))

	// Sign with the validator's key
	signerData = authsigning.SignerData{
		ChainID:       chainID,
		AccountNumber: accNum,
		Sequence:      accSeq,
	}

	// Create and add an empty signature first
	sig = signing.SignatureV2{
		PubKey: pubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signMode,
			Signature: nil,
		},
		Sequence: accSeq,
	}
	err = txBuilder.SetSignatures(sig)
	require.NoError(t, err)

	// Get the sign bytes
	signBytes, err = encCfg.TxConfig.SignModeHandler().GetSignBytes(
		signMode,
		signerData,
		txBuilder.GetTx(),
	)
	require.NoError(t, err)

	// Generate the signature
	sigBytes, _, err = keyring.Sign(record.Name, signBytes)
	require.NoError(t, err)

	// Update the signature with the actual signature bytes
	sig = signing.SignatureV2{
		PubKey: pubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signMode,
			Signature: sigBytes,
		},
		Sequence: accSeq,
	}
	err = txBuilder.SetSignatures(sig)
	require.NoError(t, err)

	// Get the transaction bytes
	authTx = txBuilder.GetTx()
	txBytes, err = encCfg.TxConfig.TxEncoder()(authTx)
	require.NoError(t, err)

	// Execute the MsgExec transaction
	header = tmproto.Header{
		Version: tmversion.Consensus{
			App: appconsts.LatestVersion,
		},
		ChainID: chainID,
		Height:  testApp.LastBlockHeight() + 1,
		Time:    time.Now(),
	}

	testApp.BeginBlock(abci.RequestBeginBlock{
		Header: header,
	})

	deliverResp = testApp.DeliverTx(abci.RequestDeliverTx{Tx: txBytes})
	t.Logf("MsgExec transaction result: code=%d, log=%s", deliverResp.Code, deliverResp.Log)
	assert.Equal(t, uint32(2), deliverResp.Code) // Code 2 is the encoding error code
	assert.Contains(t, deliverResp.Log, "call limit exceeded: tx parse error")

	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})
	testApp.Commit()
}
