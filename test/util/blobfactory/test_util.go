package blobfactory

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

func GenerateManyRawSendTxs(txConfig client.TxConfig, count int) []coretypes.Tx {
	const acc = "signer"
	kr, addr := testnode.NewKeyring(acc)
	signer, err := user.NewSigner(kr, nil, addr[0], txConfig, testfactory.ChainID, 1, 0)
	if err != nil {
		panic(err)
	}
	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		txs[i] = GenerateRawSendTx(signer, 100)
	}
	return txs
}

// GenerateRawSendTx creates send transactions meant to help test encoding/prepare/process
// proposal, they are not meant to actually be executed by the state machine. If
// we want that, we have to update nonce, and send funds to someone other than
// the same account signing the transaction.
func GenerateRawSendTx(signer *user.Signer, amount int64) []byte {
	feeCoin := sdk.Coin{
		Denom:  appconsts.BondDenom,
		Amount: sdk.NewInt(1),
	}

	opts := []user.TxOption{
		user.SetFeeAmount(sdk.NewCoins(feeCoin)),
		user.SetGasLimit(1000000000),
	}

	amountCoin := sdk.Coin{
		Denom:  appconsts.BondDenom,
		Amount: sdk.NewInt(amount),
	}

	addr := signer.Address()
	msg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(amountCoin))

	rawTx, err := signer.CreateTx([]sdk.Msg{msg}, opts...)
	if err != nil {
		panic(err)
	}
	return rawTx
}

// GenerateRandomAmount generates a random amount for a Send transaction.
func GenerateRandomAmount(rand *tmrand.Rand) int64 {
	n := rand.Int64()
	if n < 0 {
		return -n
	}
	return n
}

// GenerateRandomRawSendTx generates a random raw send tx.
func GenerateRandomRawSendTx(rand *tmrand.Rand, signer *user.Signer) (rawTx []byte) {
	amount := GenerateRandomAmount(rand)
	return GenerateRawSendTx(signer, amount)
}

// GenerateManyRandomRawSendTxsSameSigner  generates count many random raw send txs.
func GenerateManyRandomRawSendTxsSameSigner(rand *tmrand.Rand, signer *user.Signer, count int) []coretypes.Tx {
	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		txs[i] = GenerateRandomRawSendTx(rand, signer)
	}
	return txs
}
