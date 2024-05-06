package util

import (
	"math/rand"
	"testing"

	tmrand "github.com/tendermint/tendermint/libs/rand"
	"context"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	"github.com/celestiaorg/go-square/blob"
	"github.com/cosmos/cosmos-sdk/client"
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
	for i := 0; i < len(accounts); i++ {
		addr := testfactory.GetAddress(kr, accounts[i])
		acc := DirectQueryAccount(capp, addr)
		signer, err := user.NewSigner(kr, cfg, chainid, appconsts.LatestVersion, user.NewAccount(addr.String(), acc.GetAccountNumber(), acc.GetSequence()))
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
		tx, _, err := signer.CreatePayForBlobs(context.Background(), addr.String(), blobs, opts...)
		require.NoError(t, err)
		txs[i] = tx
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
		acc := user.NewAccount(addr.String(), accountNum, sequence)
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
		tx, err := signer.CreateTx([]sdk.Msg{msg}, opts...)
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

			tx , err =  signer.EncodeTx(builder.GetTx())
			require.NoError(t, err)
		}

		cTx, err := blob.MarshalBlobTx(tx, blobs...)
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
	signer, err := user.NewSigner(kr, cfg, chainid, appconsts.LatestVersion, user.NewAccount(fromAddr.String(), accountNum, sequence))
	require.NoError(t, err)

	msg := banktypes.NewMsgSend(fromAddr, toAddr, sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(amount))))
	rawTx, err := signer.CreateTx([]sdk.Msg{msg}, opts...)
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
