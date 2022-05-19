package types

import (
	"context"
	"sync"

	"github.com/celestiaorg/celestia-app/app/encoding"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/grpc"
)

// KeyringSigner uses a keyring to sign and build celestia-app transactions
type KeyringSigner struct {
	keyring.Keyring
	keyringAccName string
	accountNumber  uint64
	sequence       uint64
	chainID        string
	encCfg         encoding.EncodingConfig

	sync.RWMutex
}

// NewKeyringSigner returns a new KeyringSigner using the provided keyring
func NewKeyringSigner(ring keyring.Keyring, name string, chainID string) *KeyringSigner {
	return &KeyringSigner{
		Keyring:        ring,
		keyringAccName: name,
		chainID:        chainID,
		encCfg:         makePaymentEncodingConfig(),
	}
}

// QueryAccountNumber queries the applicaiton to find the latest account number and
// sequence, updating the respective internal fields. The internal account number must
// be set by this method or by manually calling k.SetAccountNumber in order for any built
// transactions to be valide
func (k *KeyringSigner) QueryAccountNumber(ctx context.Context, conn *grpc.ClientConn) error {
	info, err := k.Key(k.keyringAccName)
	if err != nil {
		return err
	}

	addr, err := info.GetAddress()
	if err != nil {
		return err
	}

	accNum, seqNumb, err := QueryAccount(ctx, conn, k.encCfg, addr.String())
	if err != nil {
		return err
	}
	k.Lock()
	defer k.Unlock()

	k.accountNumber = accNum
	k.sequence = seqNumb
	return nil
}

// NewTxBuilder returns the default sdk Tx builder using the celestia-app encoding config
func (k *KeyringSigner) NewTxBuilder(opts ...TxBuilderOption) sdkclient.TxBuilder {
	builder := k.encCfg.TxConfig.NewTxBuilder()
	for _, opt := range opts {
		builder = opt(builder)
	}
	return builder
}

// BuildSignedTx creates and signs a sdk.Tx that contains the provided message. The interal
// account number must be set by calling k.QueryAccountNumber or by manually setting it via
// k.SetAccountNumber for the built transactions to be valid.
func (k *KeyringSigner) BuildSignedTx(builder sdkclient.TxBuilder, msg sdktypes.Msg) (authsigning.Tx, error) {
	k.RLock()
	sequence := k.sequence
	k.RUnlock()

	// set the msg
	err := builder.SetMsgs(msg)
	if err != nil {
		return nil, err
	}

	// lookup account info
	keyInfo, err := k.Key(k.keyringAccName)
	if err != nil {
		return nil, err
	}

	pub, err := keyInfo.GetPubKey()
	if err != nil {
		return nil, err
	}

	// we must first set an empty signature in order generate
	// the correct sign bytes
	sigV2 := signing.SignatureV2{
		PubKey: pub,
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: nil,
		},
		Sequence: sequence,
	}

	// set the empty signature
	err = builder.SetSignatures(sigV2)
	if err != nil {
		return nil, err
	}

	signerData, err := k.GetSignerData()
	if err != nil {
		return nil, err
	}

	// Generate the bytes to be signed.
	bytesToSign, err := k.encCfg.TxConfig.SignModeHandler().GetSignBytes(
		signing.SignMode_SIGN_MODE_DIRECT,
		signerData,
		builder.GetTx(),
	)
	if err != nil {
		return nil, err
	}

	addr, err := keyInfo.GetAddress()
	if err != nil {
		return nil, err
	}

	// Sign those bytes using the keyring. we are ignoring the returned public key
	sigBytes, _, err := k.SignByAddress(addr, bytesToSign)
	if err != nil {
		return nil, err
	}

	// Construct the SignatureV2 struct, this time including a real signature
	sigV2 = signing.SignatureV2{
		PubKey: pub,
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: sigBytes,
		},
		Sequence: sequence,
	}

	// set the final signature
	err = builder.SetSignatures(sigV2)
	if err != nil {
		return nil, err
	}

	// return the signed transaction
	return builder.GetTx(), nil
}

// SetAccountNumber manually sets the underlying account number
func (k *KeyringSigner) SetAccountNumber(n uint64) {
	k.Lock()
	defer k.Unlock()

	k.accountNumber = n
}

// SetSequence manually sets the underlying sequence number
func (k *KeyringSigner) SetSequence(n uint64) {
	k.Lock()
	defer k.Unlock()

	k.sequence = n
}

// SetKeyringAccName manually sets the underlying keyring account name
func (k *KeyringSigner) SetKeyringAccName(name string) {
	k.keyringAccName = name
}

// GetSignerInfo returns the signer info for the KeyringSigner's account. panics
// if the account in KeyringSigner does not exist.
func (k *KeyringSigner) GetSignerInfo() *keyring.Record {
	info, err := k.Key(k.keyringAccName)
	if err != nil {
		panic(err)
	}
	return info
}

func (k *KeyringSigner) GetSignerData() (authsigning.SignerData, error) {
	k.RLock()
	accountNumber := k.accountNumber
	sequence := k.sequence
	k.RUnlock()

	record, err := k.Key(k.keyringAccName)
	if err != nil {
		return authsigning.SignerData{}, err
	}

	pubKey, err := record.GetPubKey()
	if err != nil {
		return authsigning.SignerData{}, err
	}

	address := pubKey.Address()

	return authsigning.SignerData{
		Address:       address.String(),
		ChainID:       k.chainID,
		AccountNumber: accountNumber,
		Sequence:      sequence,
		PubKey:        pubKey,
	}, nil
}

// EncodeTx uses the keyring signer's encoding config to encode the provided sdk transaction
func (k *KeyringSigner) EncodeTx(tx sdktypes.Tx) ([]byte, error) {
	return k.encCfg.TxConfig.TxEncoder()(tx)
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

// QueryAccount fetches the account number and sequence number from the celestia-app node.
func QueryAccount(ctx context.Context, conn *grpc.ClientConn, encCfg encoding.EncodingConfig, address string) (accNum uint64, seqNum uint64, err error) {
	qclient := authtypes.NewQueryClient(conn)
	resp, err := qclient.Account(
		ctx,
		&authtypes.QueryAccountRequest{Address: address},
	)
	if err != nil {
		return accNum, seqNum, err
	}

	var acc authtypes.AccountI
	err = encCfg.InterfaceRegistry.UnpackAny(resp.Account, &acc)
	if err != nil {
		return accNum, seqNum, err
	}

	accNum, seqNum = acc.GetAccountNumber(), acc.GetSequence()
	return
}
