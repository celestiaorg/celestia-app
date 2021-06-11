package client

import (
	"context"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/lazyledger/lazyledger-app/app/params"
	"google.golang.org/grpc"
)

// Builder wraps the standard sdkclient.TxBuilder add a few quality of life methods along
// with keeping track of the account number to produce transactions
type Builder struct {
	sdkclient.TxBuilder
	encCfg               params.EncodingConfig
	address              sdktypes.Address
	chainID              string
	accountNum, sequence uint64
}

func NewBuilder(address sdktypes.Address, chainID string) *Builder {
	encCfg := params.RegisterAccountInterface(params.MakeEncodingConfig())
	txBuilder := encCfg.TxConfig.NewTxBuilder()
	return &Builder{
		TxBuilder: txBuilder,
		encCfg:    encCfg,
		address:   address,
		chainID:   chainID,
	}
}

// UpdateAccountNumber queries the application to find the latest account number
// and sequence, updating the respective internal fields. This method must be
// called before building the signed transaction for the transaction to be valid.
func (b *Builder) UpdateAccountNumber(ctx context.Context, conn *grpc.ClientConn) error {
	accNum, seqNumb, err := QueryAccount(ctx, conn, b.encCfg, b.address.String())
	if err != nil {
		return err
	}
	b.accountNum = accNum
	b.sequence = seqNumb
	return nil
}

// BuildSignedTx creates a signed sdk.Tx that contains the provided WirePayForMessage.
// The internal fees, the account number, and the sequence should be updated before calling this function
// (see b.UpdateAccountNumber, sdkclient.TxBuilder.SetGasLimit, and sdkclient.TxBuilder.SetFeeAmount)
func (b *Builder) BuildSignedTx(msg sdktypes.Msg, kr keyring.Keyring) (authsigning.Tx, error) {
	// set the msg
	err := b.SetMsgs(msg)
	if err != nil {
		return nil, err
	}

	// lookup account info
	keyInfo, err := kr.KeyByAddress(b.address)
	if err != nil {
		return nil, err
	}

	// we must first set an empty signature in order generate
	// the correct sign bytes
	sigV2 := signing.SignatureV2{
		PubKey: keyInfo.GetPubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: nil,
		},
		Sequence: b.sequence,
	}

	// set the empty signature
	err = b.SetSignatures(sigV2)
	if err != nil {
		return nil, err
	}

	// Generate the bytes to be signed.
	bytesToSign, err := b.encCfg.TxConfig.SignModeHandler().GetSignBytes(
		signing.SignMode_SIGN_MODE_DIRECT,
		b.signerData(),
		b.GetTx(),
	)
	if err != nil {
		return nil, err
	}

	// Sign those bytes using the keyring. we are ignoring the returned public key
	sigBytes, _, err := kr.SignByAddress(b.address, bytesToSign)
	if err != nil {
		return nil, err
	}

	// Construct the SignatureV2 struct, this time including a real signature
	sigV2 = signing.SignatureV2{
		PubKey: keyInfo.GetPubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: sigBytes,
		},
		Sequence: b.sequence,
	}

	// set the final signature
	err = b.SetSignatures(sigV2)
	if err != nil {
		return nil, err
	}

	// return the signed transaction
	return b.GetTx(), nil
}

// EncodeTx uses the builder's encoding config to encode the provided sdk transaction
func (b Builder) EncodeTx(tx sdktypes.Tx) ([]byte, error) {
	return b.encCfg.TxConfig.TxEncoder()(tx)
}

// signerData returns the signer data from the underlying builder
func (b Builder) signerData() authsigning.SignerData {
	return authsigning.SignerData{
		ChainID:       b.chainID,
		AccountNumber: b.accountNum,
		Sequence:      b.sequence,
	}
}

// BroadcastTx uses the provided grpc connection to broadcast a signed and encoded transaction
func BroadcastTx(ctx context.Context, conn *grpc.ClientConn, mode tx.BroadcastMode, txBytes []byte) (*tx.BroadcastTxResponse, error) {
	txClient := tx.NewServiceClient(conn)

	return txClient.BroadcastTx(
		ctx,
		&tx.BroadcastTxRequest{
			Mode:    mode,
			TxBytes: txBytes,
		},
	)
}

// QueryAccount fetches the account number and sequence number from the lazyledger-app node.
func QueryAccount(ctx context.Context, conn *grpc.ClientConn, encCfg params.EncodingConfig, address string) (accNum uint64, seqNum uint64, err error) {
	qclient := authtypes.NewQueryClient(conn)
	resp, err := qclient.Account(
		ctx,
		&authtypes.QueryAccountRequest{Address: address},
	)
	if err != nil {
		return accNum, seqNum, err
	}

	var acc authtypes.AccountI
	err = encCfg.Marshaler.UnpackAny(resp.Account, &acc)
	if err != nil {
		return accNum, seqNum, err
	}

	accNum, seqNum = acc.GetAccountNumber(), acc.GetSequence()
	return
}
