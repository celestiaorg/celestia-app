package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/celestiaorg/go-square/blob"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
)

// Signer is struct for building and signing Celestia transactions
// It supports multiple accounts wrapping a Keyring.
// NOTE: All transactions may only have a single signer
// Signer is not thread-safe.
type Signer struct {
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

// NewSigner returns a new signer using the provided keyring
// There must be at least one account in the keyring
// The first account provided will be set as the default
func NewSigner(
	keys keyring.Keyring,
	encCfg client.TxConfig,
	chainID string,
	appVersion uint64,
	accounts ...*Account,
) (*Signer, error) {
	s := &Signer{
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
func (s *Signer) CreateTx(msgs []sdktypes.Msg, opts ...TxOption) ([]byte, error) {
	tx, _, _, err := s.SignTx(msgs, opts...)
	if err != nil {
		return nil, err
	}
	return s.EncodeTx(tx)
}

func (s *Signer) SignTx(msgs []sdktypes.Msg, opts ...TxOption) (authsigning.Tx, string, uint64, error) {
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

func (s *Signer) CreatePayForBlobs(ctx context.Context, account string, blobs []*blob.Blob, opts ...TxOption) ([]byte, uint64, error) {
	acc, exists := s.accounts[account]
	if !exists {
		return nil, 0, fmt.Errorf("account %s not found", account)

	}

	msg, err := blobtypes.NewMsgPayForBlobs(acc.address.String(), s.appVersion, blobs...)
	if err != nil {
		return nil, 0, err
	}

	tx, account, sequence, err := s.SignTx([]sdktypes.Msg{msg}, opts...)
	if err != nil {
		return nil, 0, err
	}

	txBytes, err := s.EncodeTx(tx)
	if err != nil {
		return nil, 0, err
	}

	blobTx, err := blob.MarshalBlobTx(txBytes, blobs...)
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

// Account returns an account of the signer from the key name. Also returns a bool if the
// account exists.
func (s *Signer) Account(name string) *Account {
	return s.accounts[name]
}

func (s *Signer) findAccount(txbuilder client.TxBuilder) (*Account, error) {
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

func (s *Signer) IncrementSequence(account string) error {
	acc, exists := s.accounts[account]
	if !exists {
		return fmt.Errorf("account %s does not exist", account)
	}
	acc.sequence++
	return nil
}

func (s *Signer) SetSequence(account string, seq uint64) error {
	acc, exists := s.accounts[account]
	if !exists {
		return fmt.Errorf("account %s does not exist", account)
	}

	acc.sequence = seq
	return nil
}

func (s *Signer) AddAccount(acc *Account) error {
	record, err := s.keys.Key(acc.name)
	if err != nil {
		return fmt.Errorf("retriveing key for account %s: %w", acc.name, err)
	}

	addr, err := record.GetAddress()
	if err != nil {
		return fmt.Errorf("getting address for key %s: %w", err)
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
func (s *Signer) Keyring() keyring.Keyring {
	return s.keys
}

func (s *Signer) signTransaction(builder client.TxBuilder) (string, uint64, error) {
	account, err := s.findAccount(builder)
	if err != nil {
		return "", 0, err
	}

	nextSequence := account.sequence + 1

	// To ensure we have the correct bytes to sign over we produce
	// a dry run of the signing data
	err = builder.SetSignatures(s.getSignatureV2(nextSequence, account.pubKey, nil))
	if err != nil {
		return "", 0, fmt.Errorf("error setting draft signatures: %w", err)
	}

	// now we can use the data to produce the signature from the signer
	signature, err := s.createSignature(builder, account, nextSequence)
	if err != nil {
		return "", 0, fmt.Errorf("error creating signature: %w", err)
	}

	err = builder.SetSignatures(s.getSignatureV2(nextSequence, account.pubKey, signature))
	if err != nil {
		return "", 0, fmt.Errorf("error setting signatures: %w", err)
	}

	return account.name, nextSequence, nil
}

func (s *Signer) createSignature(builder client.TxBuilder, account *Account, sequence uint64) ([]byte, error) {
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
func (s *Signer) txBuilder(opts ...TxOption) client.TxBuilder {
	builder := s.enc.NewTxBuilder()
	for _, opt := range opts {
		builder = opt(builder)
	}
	return builder
}

func (s *Signer) getSignatureV2(sequence uint64, pubKey cryptotypes.PubKey, signature []byte) signing.SignatureV2 {
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
