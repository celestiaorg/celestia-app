package user

import (
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmtypes "github.com/tendermint/tendermint/types"

	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
)

// TxSigner is struct for building and signing Celestia transactions
// It supports multiple accounts wrapping a Keyring.
// NOTE: All transactions may only have a single signer
// TxSigner is not thread-safe.
type TxSigner struct {
	keys    keyring.Keyring
	enc     client.TxConfig
	chainID string
	// FIXME: the signer is currently incapable of detecting an appversion
	// change and could produce incorrect PFBs if it the network is at an
	// appVersion that the signer does not support
	appVersion uint64

	// set of accounts that the signer can manage. Should match the keys on the keyring
	accounts            map[string]*Account
	addressToAccountMap map[string]string
}

// NewTxSigner returns a new signer using the provided keyring
// There must be at least one account in the keyring
// The first account provided will be set as the default
func NewTxSigner(
	keys keyring.Keyring,
	encCfg client.TxConfig,
	chainID string,
	appVersion uint64,
	accounts ...*Account,
) (*TxSigner, error) {
	s := &TxSigner{
		keys:                keys,
		chainID:             chainID,
		enc:                 encCfg,
		accounts:            make(map[string]*Account),
		addressToAccountMap: make(map[string]string),
		appVersion:          appVersion,
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
func (s *TxSigner) CreateTx(msgs []sdktypes.Msg, opts ...TxOption) ([]byte, error) {
	tx, _, _, err := s.SignTx(msgs, opts...)
	if err != nil {
		return nil, err
	}
	return s.EncodeTx(tx)
}

func (s *TxSigner) SignTx(msgs []sdktypes.Msg, opts ...TxOption) (authsigning.Tx, string, uint64, error) {
	txBuilder := s.txBuilder(opts...)
	if err := txBuilder.SetMsgs(msgs...); err != nil {
		return nil, "", 0, err
	}

	signer, sequence, err := s.signTransaction(txBuilder)
	if err != nil {
		return nil, "", 0, err
	}

	return txBuilder.GetTx(), signer, sequence, nil
}

func (s *TxSigner) CreatePayForBlobs(accountName string, blobs []*tmproto.Blob, opts ...TxOption) ([]byte, uint64, error) {
	acc, exists := s.accounts[accountName]
	if !exists {
		return nil, 0, fmt.Errorf("account %s not found", accountName)
	}

	msg, err := blobtypes.NewMsgPayForBlobs(acc.address.String(), blobs...)
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

	blobTx, err := tmtypes.MarshalBlobTx(txBytes, blobs...)
	return blobTx, sequence, err
}

func (s *TxSigner) EncodeTx(tx sdktypes.Tx) ([]byte, error) {
	return s.enc.TxEncoder()(tx)
}

func (s *TxSigner) DecodeTx(txBytes []byte) (authsigning.Tx, error) {
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
func (s *TxSigner) ChainID() string {
	return s.chainID
}

// Account returns an account of the signer from the key name
func (s *TxSigner) Account(name string) *Account {
	return s.accounts[name]
}

// AccountByAddress returns the account associated with the given address
func (s *TxSigner) AccountByAddress(address sdktypes.AccAddress) *Account {
	accountName, exists := s.addressToAccountMap[address.String()]
	if !exists {
		return nil
	}
	return s.accounts[accountName]
}

func (s *TxSigner) Accounts() []*Account {
	accounts := make([]*Account, len(s.accounts))
	i := 0
	for _, acc := range s.accounts {
		accounts[i] = acc
		i++
	}
	return accounts
}

func (s *TxSigner) findAccount(txbuilder client.TxBuilder) (*Account, error) {
	signers := txbuilder.GetTx().GetSigners()
	if len(signers) == 0 {
		return nil, fmt.Errorf("message has no signer")
	}
	accountName, exists := s.addressToAccountMap[signers[0].String()]
	if !exists {
		return nil, fmt.Errorf("account %s not found", signers[0].String())
	}
	return s.accounts[accountName], nil
}

func (s *TxSigner) IncrementSequence(accountName string) error {
	acc, exists := s.accounts[accountName]
	if !exists {
		return fmt.Errorf("account %s does not exist", accountName)
	}
	acc.sequence++
	return nil
}

func (s *TxSigner) SetSequence(accountName string, seq uint64) error {
	acc, exists := s.accounts[accountName]
	if !exists {
		return fmt.Errorf("account %s does not exist", accountName)
	}

	acc.sequence = seq
	return nil
}

func (s *TxSigner) AddAccount(acc *Account) error {
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
	s.addressToAccountMap[addr.String()] = acc.name
	return nil
}

// Keyring exposes the signers underlying keyring
func (s *TxSigner) Keyring() keyring.Keyring {
	return s.keys
}

func (s *TxSigner) signTransaction(builder client.TxBuilder) (string, uint64, error) {
	account, err := s.findAccount(builder)
	if err != nil {
		return "", 0, err
	}

	// To ensure we have the correct bytes to sign over we produce
	// a dry run of the signing data
	err = builder.SetSignatures(s.getSignatureV2(account.sequence, account.pubKey, nil))
	if err != nil {
		return "", 0, fmt.Errorf("error setting draft signatures: %w", err)
	}

	// now we can use the data to produce the signature from the signer
	signature, err := s.createSignature(builder, account, account.sequence)
	if err != nil {
		return "", 0, fmt.Errorf("error creating signature: %w", err)
	}

	err = builder.SetSignatures(s.getSignatureV2(account.sequence, account.pubKey, signature))
	if err != nil {
		return "", 0, fmt.Errorf("error setting signatures: %w", err)
	}

	return account.name, account.sequence, nil
}

func (s *TxSigner) createSignature(builder client.TxBuilder, account *Account, sequence uint64) ([]byte, error) {
	signerData := authsigning.SignerData{
		Address:       account.address.String(),
		ChainID:       s.ChainID(),
		AccountNumber: account.accountNumber,
		Sequence:      sequence,
		PubKey:        account.pubKey,
	}

	bytesToSign, err := s.enc.SignModeHandler().GetSignBytes(
		signing.SignMode_SIGN_MODE_DIRECT,
		signerData,
		builder.GetTx(),
	)
	if err != nil {
		return nil, fmt.Errorf("error getting sign bytes: %w", err)
	}

	signature, _, err := s.keys.Sign(account.name, bytesToSign)
	if err != nil {
		return nil, fmt.Errorf("error signing bytes: %w", err)
	}

	return signature, nil
}

// txBuilder returns the default sdk Tx builder using the celestia-app encoding config
func (s *TxSigner) txBuilder(opts ...TxOption) client.TxBuilder {
	builder := s.enc.NewTxBuilder()
	for _, opt := range opts {
		builder = opt(builder)
	}
	return builder
}

func (s *TxSigner) getSignatureV2(sequence uint64, pubKey cryptotypes.PubKey, signature []byte) signing.SignatureV2 {
	sigV2 := signing.SignatureV2{
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: signature,
		},
		Sequence: sequence,
	}
	if sequence == 0 {
		sigV2.PubKey = pubKey
	}
	return sigV2
}
