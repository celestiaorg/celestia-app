package blobfactory

import (
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

func DefaultTxOpts() []user.TxOption {
	return FeeTxOpts(10_000_000)
}

func FeeTxOpts(gas uint64) []user.TxOption {
	fee := uint64(float64(gas)*appconsts.DefaultMinGasPrice) + 1
	return []user.TxOption{
		user.SetFee(fee),
		user.SetGasLimit(gas),
	}
}

func GenerateManyRawSendTxs(signer *user.Signer, count int) []coretypes.Tx {
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
	opts := DefaultTxOpts()

	amountCoin := sdk.Coin{
		Denom:  appconsts.BondDenom,
		Amount: sdk.NewInt(amount),
	}

	addr := signer.Address()
	msg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(amountCoin))

	tx, err := signer.CreateTx([]sdk.Msg{msg}, opts...)
	if err != nil {
		panic(err)
	}

	rawTx, err := signer.EncodeTx(tx)
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
