package util

import (
	"math/rand"
	"testing"

	"cosmossdk.io/math"
	tmrand "cosmossdk.io/math/unsafe"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/go-square/v2/tx"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"
)

// RandBlobTxsWithAccounts will create random blob transactions using the
// provided configuration. The account info is queried directly from the
// application. One blob transaction is generated per account provided.
func RandBlobTxsWithAccounts(
	t *testing.T,
	capp *app.App,
	cfg client.TxConfig,
	kr keyring.Keyring,
	size int,
	blobCount int,
	randSize bool,
	chainid string,
	accounts []string,
	extraOpts ...user.TxOption,
) []coretypes.Tx {
	opts := append(blobfactory.DefaultTxOpts(), extraOpts...)

	require.Greater(t, size, 0)
	require.Greater(t, blobCount, 0)

	txs := make([]coretypes.Tx, len(accounts))

	appVersion, err := capp.AppVersion(capp.NewContext(true))
	require.NoError(t, err)

	for i := 0; i < len(accounts); i++ {
		addr := testfactory.GetAddress(kr, accounts[i])
		acc := DirectQueryAccount(capp, addr)
		account := user.NewAccount(accounts[i], acc.GetAccountNumber(), acc.GetSequence())
		signer, err := user.NewSigner(kr, cfg, chainid, appVersion, account)
		require.NoError(t, err)

		randomizedSize := size
		if randSize {
			randomizedSize = rand.Intn(size)
			if randomizedSize == 0 {
				randomizedSize = 1
			}
		}
		randomizedBlobCount := blobCount
		if randSize {
			randomizedBlobCount = rand.Intn(blobCount)
			if randomizedBlobCount == 0 {
				randomizedBlobCount = 1
			}
		}

		_, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), addr.String(), randomizedSize, randomizedBlobCount)
		tx, _, err := signer.CreatePayForBlobs(account.Name(), blobs, opts...)
		require.NoError(t, err)
		txs[i] = tx
	}

	return txs
}

func DirectQueryAccount(app *app.App, addr sdk.AccAddress) sdk.AccountI {
	ctx := app.NewContext(true)
	return app.AccountKeeper.GetAccount(ctx, addr)
}

// RandBlobTxsWithManualSequence will create random blob transactions using the
// provided configuration. One blob transaction is generated per account
// provided. The sequence and account numbers are set manually using the provided values.
func RandBlobTxsWithManualSequence(
	t *testing.T,
	cfg client.TxConfig,
	kr keyring.Keyring,
	size int,
	blobCount int,
	randSize bool,
	chainid string,
	accounts []string,
	sequence, accountNum uint64,
	invalidSignature bool,
) []coretypes.Tx {
	t.Helper()
	require.Greater(t, size, 0)
	require.Greater(t, blobCount, 0)

	opts := blobfactory.DefaultTxOpts()
	txs := make([]coretypes.Tx, len(accounts))
	for i := 0; i < len(accounts); i++ {
		addr := testfactory.GetAddress(kr, accounts[i])
		acc := user.NewAccount(accounts[i], accountNum, sequence)
		signer, err := user.NewSigner(kr, cfg, chainid, appconsts.LatestVersion, acc)
		require.NoError(t, err)

		randomizedSize := size
		if randSize {
			randomizedSize = rand.Intn(size)
			if randomizedSize == 0 {
				randomizedSize = 1
			}
		}

		randomizedBlobCount := blobCount
		if randSize {
			randomizedBlobCount = rand.Intn(blobCount)
			if randomizedBlobCount == 0 {
				randomizedBlobCount = 1
			}
		}

		msg, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), addr.String(), randomizedSize, randomizedBlobCount)
		transaction, _, err := signer.CreateTx([]sdk.Msg{msg}, opts...)
		require.NoError(t, err)
		if invalidSignature {
			builder := cfg.NewTxBuilder()
			for _, opt := range opts {
				builder = opt(builder)
			}
			require.NoError(t, builder.SetMsgs(msg))
			err := builder.SetSignatures(signing.SignatureV2{
				PubKey: acc.PubKey(),
				Data: &signing.SingleSignatureData{
					SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
					Signature: []byte("invalid signature"),
				},
				Sequence: acc.Sequence(),
			})
			require.NoError(t, err)

			transaction, err = signer.EncodeTx(builder.GetTx())
			require.NoError(t, err)
		}

		cTx, err := tx.MarshalBlobTx(transaction, blobs...)
		if err != nil {
			panic(err)
		}
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
	enc client.TxConfig,
	kr keyring.Keyring,
	amount uint64,
	toAccount string,
	accounts []string,
	chainid string,
	extraOpts ...user.TxOption,
) []coretypes.Tx {
	opts := append(blobfactory.DefaultTxOpts(), extraOpts...)

	txs := make([]coretypes.Tx, len(accounts))
	for i := 0; i < len(accounts); i++ {
		signingAddr := getAddress(accounts[i], kr)

		// update the account info in the signer so the signature is valid
		acc := DirectQueryAccount(capp, signingAddr)
		if acc == nil {
			t.Fatalf("account %s not found", signingAddr)
		}

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
	cfg client.TxConfig,
	kr keyring.Keyring,
	fromAcc, toAcc string,
	amount uint64,
	chainid string,
	sequence, accountNum uint64,
	opts ...user.TxOption,
) coretypes.Tx {
	fromAddr, toAddr := getAddress(fromAcc, kr), getAddress(toAcc, kr)
	signer, err := user.NewSigner(kr, cfg, chainid, appconsts.LatestVersion, user.NewAccount(fromAcc, accountNum, sequence))
	require.NoError(t, err)

	msg := banktypes.NewMsgSend(fromAddr, toAddr, sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewIntFromUint64(amount))))
	rawTx, _, err := signer.CreateTx([]sdk.Msg{msg}, opts...)
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
