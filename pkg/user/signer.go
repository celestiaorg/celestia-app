package user

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/core/address"
	"github.com/cosmos/cosmos-sdk/client"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/grpc"

	"github.com/celestiaorg/go-square/v2/share"
	blobtx "github.com/celestiaorg/go-square/v2/tx"

	"github.com/celestiaorg/celestia-app/v4/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v4/app/params"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

var defaultSignMode = signing.SignMode_SIGN_MODE_DIRECT

// Signer is struct for building and signing Celestia transactions
// It supports multiple accounts wrapping a Keyring.
// NOTE: All transactions may only have a single signer
// Signer is not thread-safe.
type Signer struct {
	keys         keyring.Keyring
	enc          client.TxConfig
	addressCodec address.Codec
	chainID      string
	// set of accounts that the signer can manage. Should match the keys on the keyring
	accounts            map[string]*Account
	addressToAccountMap map[string]string
}

// NewSigner returns a new signer using the provided keyring
// There must be at least one account in the keyring
// The first account provided will be set as the default
func NewSigner(keys keyring.Keyring, encCfg client.TxConfig, chainID string, accounts ...*Account) (*Signer, error) {
	s := &Signer{
		keys:                keys,
		chainID:             chainID,
		enc:                 encCfg,
		addressCodec:        addresscodec.NewBech32Codec(params.Bech32PrefixAccAddr),
		accounts:            make(map[string]*Account),
		addressToAccountMap: make(map[string]string),
	}

	for _, acc := range accounts {
		if err := s.AddAccount(acc); err != nil {
			return nil, err
		}
	}

	return s, nil
}

// CreateTx forms a transaction from the provided messages and signs it.
// TxOptions may be optionally used to set the gas limit and fee.
func (s *Signer) CreateTx(msgs []sdktypes.Msg, opts ...TxOption) ([]byte, authsigning.Tx, error) {
	tx, _, _, err := s.SignTx(msgs, opts...)
	if err != nil {
		return nil, nil, err
	}

	blob, err := s.EncodeTx(tx)

	return blob, tx, err
}

func (s *Signer) SignTx(msgs []sdktypes.Msg, opts ...TxOption) (authsigning.Tx, string, uint64, error) {
	txBuilder, err := s.txBuilder(msgs, opts...)
	if err != nil {
		return nil, "", 0, err
	}

	signer, sequence, err := s.signTransaction(txBuilder)
	if err != nil {
		return nil, "", 0, err
	}

	return txBuilder.GetTx(), signer, sequence, nil
}

func (s *Signer) CreatePayForBlobs(accountName string, blobs []*share.Blob, opts ...TxOption) ([]byte, uint64, error) {
	acc, exists := s.accounts[accountName]
	if !exists {
		return nil, 0, fmt.Errorf("account %s not found", accountName)
	}

	addr, err := s.addressCodec.BytesToString(acc.address)
	if err != nil {
		return nil, 0, err
	}

	msg, err := blobtypes.NewMsgPayForBlobs(addr, appconsts.LatestVersion, blobs...)
	if err != nil {
		return nil, 0, err
	}

	tx, _, sequence, err := s.SignTx([]sdktypes.Msg{msg}, opts...)
	if err != nil {
		return nil, 0, err
	}

	txBytes, err := s.EncodeTx(tx)
	if err != nil {
		return nil, 0, err
	}

	blobTx, err := blobtx.MarshalBlobTx(txBytes, blobs...)
	return blobTx, sequence, err
}

func (s *Signer) EncodeTx(tx sdktypes.Tx) ([]byte, error) {
	return s.enc.TxEncoder()(tx)
}

func (s *Signer) DecodeTx(txBytes []byte) (authsigning.Tx, error) {
	tx, err := s.enc.TxDecoder()(txBytes)
	if err != nil {
		return nil, err
	}
	authTx, ok := tx.(authsigning.Tx)
	if !ok {
		return nil, errors.New("not an authsigning transaction")
	}
	return authTx, nil
}

// ChainID returns the chain ID of the signer.
func (s *Signer) ChainID() string {
	return s.chainID
}

// Account returns an account of the signer from the key name
func (s *Signer) Account(name string) *Account {
	return s.accounts[name]
}

// AccountByAddress returns the account associated with the given address
func (s *Signer) AccountByAddress(address sdktypes.AccAddress) *Account {
	addrStr, err := s.addressCodec.BytesToString(address)
	if err != nil {
		return nil
	}

	accountName, exists := s.addressToAccountMap[addrStr]
	if !exists {
		return nil
	}
	return s.accounts[accountName]
}

func (s *Signer) Accounts() []*Account {
	accounts := make([]*Account, len(s.accounts))
	i := 0
	for _, acc := range s.accounts {
		accounts[i] = acc
		i++
	}
	return accounts
}

func (s *Signer) findAccount(txbuilder client.TxBuilder) (*Account, error) {
	signers, err := txbuilder.GetTx().GetSigners()
	if err != nil {
		return nil, fmt.Errorf("error getting signers: %w", err)
	}

	if len(signers) == 0 {
		return nil, fmt.Errorf("message has no signer")
	}

	signerStr, err := s.addressCodec.BytesToString(signers[0])
	if err != nil {
		return nil, fmt.Errorf("error converting signer to string: %w", err)
	}

	accountName, exists := s.addressToAccountMap[signerStr]
	if !exists {
		return nil, fmt.Errorf("account %s not found", signerStr)
	}
	return s.accounts[accountName], nil
}

func (s *Signer) IncrementSequence(accountName string) error {
	acc, exists := s.accounts[accountName]
	if !exists {
		return fmt.Errorf("account %s does not exist", accountName)
	}
	acc.sequence++
	return nil
}

func (s *Signer) SetSequence(accountName string, seq uint64) error {
	acc, exists := s.accounts[accountName]
	if !exists {
		return fmt.Errorf("account %s does not exist", accountName)
	}

	acc.sequence = seq
	return nil
}

func (s *Signer) AddAccount(acc *Account) error {
	if acc == nil {
		return errors.New("account is nil")
	}

	record, err := s.keys.Key(acc.name)
	if err != nil {
		return fmt.Errorf("retrieving key for account %s: %w", acc.name, err)
	}

	addr, err := record.GetAddress()
	if err != nil {
		return fmt.Errorf("getting address for key %s: %w", acc.pubKey, err)
	}

	pk, err := record.GetPubKey()
	if err != nil {
		return fmt.Errorf("getting public key for account %s: %w", acc.name, err)
	}

	acc.address = addr
	acc.pubKey = pk
	s.accounts[acc.name] = acc

	addrStr, err := s.addressCodec.BytesToString(addr)
	if err != nil {
		return nil
	}

	s.addressToAccountMap[addrStr] = acc.name
	return nil
}

// Keyring exposes the signers underlying keyring
func (s *Signer) Keyring() keyring.Keyring {
	return s.keys
}

func (s *Signer) signTransaction(builder client.TxBuilder) (string, uint64, error) {
	account, err := s.findAccount(builder)
	if err != nil {
		return "", 0, err
	}

	// a dry run of the signing data
	err = builder.SetSignatures(signing.SignatureV2{
		Data: &signing.SingleSignatureData{
			SignMode:  defaultSignMode,
			Signature: nil,
		},
		PubKey:   account.pubKey,
		Sequence: account.sequence,
	})
	if err != nil {
		return "", 0, fmt.Errorf("error setting draft signatures: %w", err)
	}

	signature, err := s.createSignature(builder, account, account.sequence)
	if err != nil {
		return "", 0, fmt.Errorf("error creating signature: %w", err)
	}

	err = builder.SetSignatures(signing.SignatureV2{
		Data: &signing.SingleSignatureData{
			SignMode:  defaultSignMode,
			Signature: signature,
		},
		PubKey:   account.pubKey,
		Sequence: account.sequence,
	})
	if err != nil {
		return "", 0, fmt.Errorf("error setting signatures: %w", err)
	}

	return account.name, account.sequence, nil
}

func (s *Signer) createSignature(builder client.TxBuilder, account *Account, sequence uint64) ([]byte, error) {
	addrStr, err := s.addressCodec.BytesToString(account.address)
	if err != nil {
		return nil, fmt.Errorf("error converting address to string: %w", err)
	}

	signerData := authsigning.SignerData{
		Address:       addrStr,
		ChainID:       s.ChainID(),
		AccountNumber: account.accountNumber,
		Sequence:      sequence,
		PubKey:        account.pubKey,
	}

	bytesToSign, err := authsigning.GetSignBytesAdapter(context.Background(), s.enc.SignModeHandler(), defaultSignMode, signerData, builder.GetTx())
	if err != nil {
		return nil, fmt.Errorf("error getting sign bytes: %w", err)
	}
	signature, _, err := s.keys.Sign(account.name, bytesToSign, defaultSignMode)
	if err != nil {
		return nil, fmt.Errorf("error signing bytes: %w", err)
	}

	return signature, nil
}

// txBuilder returns the default sdk Tx builder using the celestia-app encoding config
func (s *Signer) txBuilder(msgs []sdktypes.Msg, opts ...TxOption) (client.TxBuilder, error) {
	builder := s.enc.NewTxBuilder()
	if err := builder.SetMsgs(msgs...); err != nil {
		return nil, err
	}

	for _, opt := range opts {
		builder = opt(builder)
	}
	return builder, nil
}

// QueryGasPrice takes a priority and an app gRPC client.
// Returns the current network gas price corresponding to the provided priority.
// More on the gas price estimation can be found in docs/architecture/adr-023-gas-used-and-gas-price-estimation.md
func (s *Signer) QueryGasPrice(
	ctx context.Context,
	grpcClient *grpc.ClientConn,
	priority gasestimation.TxPriority,
) (float64, error) {
	estimator := gasestimation.NewGasEstimatorClient(grpcClient)
	gasPrice, err := estimator.EstimateGasPrice(
		ctx,
		&gasestimation.EstimateGasPriceRequest{TxPriority: priority},
	)
	if err != nil {
		return 0, err
	}
	return gasPrice.EstimatedGasPrice, nil
}

// QueryGasUsedAndPrice takes a priority, an app gRPC client, and a transaction bytes.
// Returns the current network gas price corresponding to the provided priority,
// and the gas used estimation for the provided transaction bytes.
// More on the gas estimation can be found in docs/architecture/adr-023-gas-used-and-gas-price-estimation.md
func (s *Signer) QueryGasUsedAndPrice(
	ctx context.Context,
	grpcClient *grpc.ClientConn,
	priority gasestimation.TxPriority,
	txBytes []byte,
) (float64, uint64, error) {
	estimator := gasestimation.NewGasEstimatorClient(grpcClient)
	gasEstimation, err := estimator.EstimateGasPriceAndUsage(
		ctx,
		&gasestimation.EstimateGasPriceAndUsageRequest{
			TxPriority: priority,
			TxBytes:    txBytes,
		},
	)
	if err != nil {
		return 0, 0, err
	}
	return gasEstimation.EstimatedGasPrice, gasEstimation.EstimatedGasUsed, nil
}
