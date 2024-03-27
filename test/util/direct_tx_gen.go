package util

import (
	"math/rand"
	"testing"

	tmrand "github.com/tendermint/tendermint/libs/rand"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// RandBlobTxsWithAccounts will create random blob transactions using the
// provided configuration. The account info is queried directly from the
// application. One blob transaction is generated per account provided.
func RandBlobTxsWithAccounts(
	_ *testing.T,
	capp *app.App,
	_ sdk.TxEncoder,
	kr keyring.Keyring,
	size int,
	blobCount int,
	randSize bool,
	chainid string,
	accounts []string,
	extraOpts ...blobtypes.TxBuilderOption,
) []coretypes.Tx {
	coin := sdk.Coin{
		Denom:  app.BondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []blobtypes.TxBuilderOption{
		blobtypes.SetFeeAmount(sdk.NewCoins(coin)),
		blobtypes.SetGasLimit(100000000000000),
	}
	opts = append(opts, extraOpts...)

	txs := make([]coretypes.Tx, len(accounts))
	for i := 0; i < len(accounts); i++ {
		signer := blobtypes.NewKeyringSigner(kr, accounts[i], chainid)

		addr, err := signer.GetSignerInfo().GetAddress()
		if err != nil {
			panic(err)
		}

		// update the account info in the signer so the signature is valid
		acc := DirectQueryAccount(capp, addr)
		signer.SetAccountNumber(0)
		signer.SetSequence(acc.GetSequence())

		if size <= 0 {
			panic("size should be positive")
		}
		randomizedSize := size
		if randSize {
			randomizedSize = rand.Intn(size)
			if randomizedSize == 0 {
				randomizedSize = 1
			}
		}
		if blobCount <= 0 {
			panic("blobCount should be strictly positive")
		}
		randomizedBlobCount := blobCount
		if randSize {
			randomizedBlobCount = rand.Intn(blobCount)
			if randomizedBlobCount == 0 {
				randomizedBlobCount = 1
			}
		}

		msg, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), addr.String(), randomizedSize, randomizedBlobCount)
		builder := signer.NewTxBuilder(opts...)
		stx, err := signer.BuildSignedTx(builder, msg)
		if err != nil {
			panic(err)
		}

		rawTx, err := signer.EncodeTx(stx)
		if err != nil {
			panic(err)
		}
		cTx, err := coretypes.MarshalBlobTx(rawTx, blobs...)
		if err != nil {
			panic(err)
		}

		txs[i] = cTx
	}

	return txs
}

func DirectQueryAccount(app *app.App, addr sdk.AccAddress) authtypes.AccountI {
	ctx := app.NewContext(true, tmproto.Header{})
	return app.AccountKeeper.GetAccount(ctx, addr)
}

// RandBlobTxsWithManualSequence will create random blob transactions using the
// provided configuration. One blob transaction is generated per account
// provided. The sequence and account numbers are set manually using the provided values.
func RandBlobTxsWithManualSequence(
	t *testing.T,
	_ sdk.TxEncoder,
	kr keyring.Keyring,
	size int,
	blobCount int,
	randSize bool,
	chainid string,
	accounts []string,
	sequence, accountNum uint64,
	invalidSignature bool,
) []coretypes.Tx {
	coin := sdk.Coin{
		Denom:  app.BondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []blobtypes.TxBuilderOption{
		blobtypes.SetFeeAmount(sdk.NewCoins(coin)),
		blobtypes.SetGasLimit(100000000000000),
	}

	txs := make([]coretypes.Tx, len(accounts))
	for i := 0; i < len(accounts); i++ {
		signer := blobtypes.NewKeyringSigner(kr, accounts[i], chainid)

		addr, err := signer.GetSignerInfo().GetAddress()
		if err != nil {
			panic(err)
		}

		signer.SetAccountNumber(accountNum)
		signer.SetSequence(sequence)

		if size <= 0 {
			panic("size should be positive")
		}
		randomizedSize := size
		if randSize {
			randomizedSize = rand.Intn(size)
			if randomizedSize == 0 {
				randomizedSize = 1
			}
		}
		if blobCount <= 0 {
			panic("blobCount should be strictly positive")
		}
		randomizedBlobCount := blobCount
		if randSize {
			randomizedBlobCount = rand.Intn(blobCount)
			if randomizedBlobCount == 0 {
				randomizedBlobCount = 1
			}
		}
		msg, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), addr.String(), randomizedSize, randomizedBlobCount)
		builder := signer.NewTxBuilder(opts...)
		stx, err := signer.BuildSignedTx(builder, msg)
		if err != nil {
			panic(err)
		}
		if invalidSignature {
			invalidSig, err := builder.GetTx().GetSignaturesV2()
			require.NoError(t, err)
			invalidSig[0].Data.(*signing.SingleSignatureData).Signature = []byte("invalid signature")

			err = builder.SetSignatures(invalidSig...)
			require.NoError(t, err)

			stx = builder.GetTx()
		}
		rawTx, err := signer.EncodeTx(stx)
		require.NoError(t, err)

		cTx, err := coretypes.MarshalBlobTx(rawTx, blobs...)
		require.NoError(t, err)
		txs[i] = cTx
	}

	return txs
}

// SendTxsWithAccounts will create a send transaction per account provided, and
// send all funds to the "toAccount". The account info is queried directly from
// the application.
func SendTxsWithAccounts(
	t *testing.T,
	capp *app.App,
	enc sdk.TxEncoder,
	kr keyring.Keyring,
	amount uint64,
	toAccount string,
	accounts []string,
	chainid string,
	extraOpts ...blobtypes.TxBuilderOption,
) []coretypes.Tx {
	coin := sdk.Coin{
		Denom:  app.BondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []blobtypes.TxBuilderOption{
		blobtypes.SetFeeAmount(sdk.NewCoins(coin)),
		blobtypes.SetGasLimit(1000000),
	}
	opts = append(opts, extraOpts...)

	txs := make([]coretypes.Tx, len(accounts))
	for i := 0; i < len(accounts); i++ {
		signingAddr := getAddress(accounts[i], kr)

		// update the account info in the signer so the signature is valid
		acc := DirectQueryAccount(capp, signingAddr)

		txs[i] = SendTxWithManualSequence(
			t,
			enc,
			kr,
			accounts[i],
			toAccount,
			amount,
			chainid,
			acc.GetSequence(),
			acc.GetAccountNumber(),
			opts...,
		)
	}

	return txs
}

// SendTxsWithAccounts will create a send transaction per account provided. The
// account info must be provided.
func SendTxWithManualSequence(
	t *testing.T,
	_ sdk.TxEncoder,
	kr keyring.Keyring,
	fromAcc, toAcc string,
	amount uint64,
	chainid string,
	sequence, accountNum uint64,
	opts ...blobtypes.TxBuilderOption,
) coretypes.Tx {
	signer := blobtypes.NewKeyringSigner(kr, fromAcc, chainid)

	signer.SetAccountNumber(accountNum)
	signer.SetSequence(sequence)

	fromAddr, toAddr := getAddress(fromAcc, kr), getAddress(toAcc, kr)

	msg := banktypes.NewMsgSend(fromAddr, toAddr, sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(amount))))

	stx, err := signer.BuildSignedTx(signer.NewTxBuilder(opts...), msg)
	require.NoError(t, err)

	rawTx, err := signer.EncodeTx(stx)
	require.NoError(t, err)

	return rawTx
}

func getAddress(account string, kr keyring.Keyring) sdk.AccAddress {
	rec, err := kr.Key(account)
	if err != nil {
		panic(err)
	}
	addr, err := rec.GetAddress()
	if err != nil {
		panic(err)
	}
	return addr
}
