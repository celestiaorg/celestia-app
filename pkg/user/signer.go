package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/blob"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/grpc"
)

const DefaultPollTime = 3 * time.Second

// Signer is an abstraction for building, signing, and broadcasting Celestia transactions
type Signer struct {
	keys          keyring.Keyring
	address       sdktypes.AccAddress
	enc           client.TxConfig
	grpc          *grpc.ClientConn
	pk            cryptotypes.PubKey
	chainID       string
	accountNumber uint64
	pollTime      time.Duration

	mtx                   sync.RWMutex
	lastSignedSequence    uint64
	lastConfirmedSequence uint64
}

// NewSigner returns a new signer using the provided keyring
func NewSigner(
	keys keyring.Keyring,
	conn *grpc.ClientConn,
	address sdktypes.AccAddress,
	enc client.TxConfig,
	chainID string,
	accountNumber uint64,
	sequence uint64,
) (*Signer, error) {
	// check that the address exists
	record, err := keys.KeyByAddress(address)
	if err != nil {
		return nil, err
	}

	pk, err := record.GetPubKey()
	if err != nil {
		return nil, err
	}

	return &Signer{
		keys:                  keys,
		address:               address,
		grpc:                  conn,
		enc:                   enc,
		pk:                    pk,
		chainID:               chainID,
		accountNumber:         accountNumber,
		lastSignedSequence:    sequence,
		lastConfirmedSequence: sequence,
		pollTime:              DefaultPollTime,
	}, nil
}

// SetupSingleSigner sets up a signer based on the provided keyring. The keyring
// must contain exactly one key. It extracts the address from the key and uses
// the grpc connection to populate the chainID, account number, and sequence
// number.
func SetupSingleSigner(ctx context.Context, keys keyring.Keyring, conn *grpc.ClientConn, encCfg encoding.Config) (*Signer, error) {
	records, err := keys.List()
	if err != nil {
		return nil, err
	}

	if len(records) != 1 {
		return nil, errors.New("keyring must contain exactly one key")
	}

	address, err := records[0].GetAddress()
	if err != nil {
		return nil, err
	}

	return SetupSigner(ctx, keys, conn, address, encCfg)
}

// SetupSigner uses the underlying grpc connection to populate the chainID, accountNumber and sequence number of the
// account.
func SetupSigner(
	ctx context.Context,
	keys keyring.Keyring,
	conn *grpc.ClientConn,
	address sdktypes.AccAddress,
	encCfg encoding.Config,
) (*Signer, error) {
	resp, err := tmservice.NewServiceClient(conn).GetLatestBlock(ctx, &tmservice.GetLatestBlockRequest{})
	if err != nil {
		return nil, err
	}

	chainID := resp.SdkBlock.Header.ChainID
	accNum, seqNum, err := QueryAccount(ctx, conn, encCfg, address.String())
	if err != nil {
		return nil, err
	}

	return NewSigner(keys, conn, address, encCfg.TxConfig, chainID, accNum, seqNum)
}

// SubmitTx forms a transaction from the provided messages, signs it, and submits it to the chain. TxOptions
// may be provided to set the fee and gas limit.
func (s *Signer) SubmitTx(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (*sdktypes.TxResponse, error) {
	txBytes, err := s.CreateTx(msgs, opts...)
	if err != nil {
		return nil, err
	}

	resp, err := s.BroadcastTx(ctx, txBytes)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return resp, fmt.Errorf("tx failed with code %d: %s", resp.Code, resp.RawLog)
	}

	return s.ConfirmTx(ctx, resp.TxHash)
}

// SubmitPayForBlob forms a transaction from the provided blobs, signs it, and submits it to the chain.
// TxOptions may be provided to set the fee and gas limit.
func (s *Signer) SubmitPayForBlob(ctx context.Context, blobs []*blob.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	txBytes, err := s.CreatePayForBlob(blobs, opts...)
	if err != nil {
		return nil, err
	}

	resp, err := s.BroadcastTx(ctx, txBytes)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return resp, fmt.Errorf("tx failed with code %d: %s", resp.Code, resp.RawLog)
	}

	return s.ConfirmTx(ctx, resp.TxHash)
}

// CreateTx forms a transaction from the provided messages and signs it. TxOptions may be optionally
// used to set the gas limit and fee.
func (s *Signer) CreateTx(msgs []sdktypes.Msg, opts ...TxOption) ([]byte, error) {
	txBuilder := s.txBuilder(opts...)
	if err := txBuilder.SetMsgs(msgs...); err != nil {
		return nil, err
	}

	if err := s.signTransaction(txBuilder); err != nil {
		return nil, err
	}

	return s.enc.TxEncoder()(txBuilder.GetTx())
}

func (s *Signer) CreatePayForBlob(blobs []*blob.Blob, opts ...TxOption) ([]byte, error) {
	msg, err := blobtypes.NewMsgPayForBlobs(s.address.String(), blobs...)
	if err != nil {
		return nil, err
	}

	txBytes, err := s.CreateTx([]sdktypes.Msg{msg}, opts...)
	if err != nil {
		return nil, err
	}

	return blob.MarshalBlobTx(txBytes, blobs...)
}

// BroadcastTx submits the provided transaction bytes to the chain and returns the response.
func (s *Signer) BroadcastTx(ctx context.Context, txBytes []byte) (*sdktypes.TxResponse, error) {
	txClient := tx.NewServiceClient(s.grpc)

	// TODO (@cmwaters): handle nonce mismatch errors
	resp, err := txClient.BroadcastTx(
		ctx,
		&tx.BroadcastTxRequest{
			Mode:    tx.BroadcastMode_BROADCAST_MODE_SYNC,
			TxBytes: txBytes,
		},
	)
	if err != nil {
		return nil, err
	}
	return resp.TxResponse, nil
}

// ConfirmTx periodically pings the provided node for the commitment of a transaction by its
// hash. It will continually loop until the context is cancelled, the tx is found or an error
// is encountered.
func (s *Signer) ConfirmTx(ctx context.Context, txHash string) (*sdktypes.TxResponse, error) {
	txClient := tx.NewServiceClient(s.grpc)
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return &sdktypes.TxResponse{}, ctx.Err()
		case <-timer.C:
			resp, err := txClient.GetTx(
				ctx,
				&tx.GetTxRequest{
					Hash: txHash,
				},
			)
			if err == nil {
				if resp.TxResponse.Code != 0 {
					return resp.TxResponse, fmt.Errorf("tx failed with code %d: %s", resp.TxResponse.Code, resp.TxResponse.RawLog)
				}
				return resp.TxResponse, nil
			}

			if !strings.Contains(err.Error(), "not found") {
				return &sdktypes.TxResponse{}, err
			}

			timer.Reset(s.pollTime)
		}
	}
}

// ChainID returns the chain ID of the signer.
func (s *Signer) ChainID() string {
	return s.chainID
}

// AccountNumber returns the account number of the signer.
func (s *Signer) AccountNumber() uint64 {
	return s.accountNumber
}

// Address returns the address of the signer.
func (s *Signer) Address() sdktypes.AccAddress {
	return s.address
}

// SetPollTime sets how often the signer should poll for the confirmation of the transaction
func (s *Signer) SetPollTime(pollTime time.Duration) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.pollTime = pollTime
}

// PubKey returns the public key of the signer
func (s *Signer) PubKey() cryptotypes.PubKey {
	return s.pk
}

// GetSequence gets the latest signed sequence and increments the local sequence number
func (s *Signer) GetSequence() uint64 {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	defer func() { s.lastSignedSequence++ }()
	return s.lastSignedSequence
}

// ForceSetSequence manually overrides the current sequence number. Be careful when
// invoking this as it may cause the transactions to reject the sequence if
// it doesn't match the one in state
func (s *Signer) ForceSetSequence(seq uint64) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.lastSignedSequence = seq
}

func (s *Signer) signTransaction(builder client.TxBuilder) error {
	signers := builder.GetTx().GetSigners()
	if len(signers) != 1 {
		return fmt.Errorf("expected 1 signer, got %d", len(signers))
	}

	if !s.address.Equals(signers[0]) {
		return fmt.Errorf("expected signer %s, got %s", s.address.String(), signers[0].String())
	}

	sequence := s.GetSequence()

	// To ensure we have the correct bytes to sign over we produce
	// a dry run of the signing data
	draftsigV2 := signing.SignatureV2{
		PubKey: s.pk,
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: nil,
		},
		Sequence: sequence,
	}

	err := builder.SetSignatures(draftsigV2)
	if err != nil {
		return fmt.Errorf("error setting draft signatures: %w", err)
	}

	// now we can use the data to produce the signature from the signer
	signature, err := s.createSignature(builder, sequence)
	if err != nil {
		return fmt.Errorf("error creating signature: %w", err)
	}
	sigV2 := signing.SignatureV2{
		PubKey: s.pk,
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: signature,
		},
		Sequence: sequence,
	}

	err = builder.SetSignatures(sigV2)
	if err != nil {
		return fmt.Errorf("error setting signatures: %w", err)
	}

	return nil
}

func (s *Signer) createSignature(builder client.TxBuilder, sequence uint64) ([]byte, error) {
	signerData := authsigning.SignerData{
		Address:       s.address.String(),
		ChainID:       s.ChainID(),
		AccountNumber: s.accountNumber,
		Sequence:      sequence,
		PubKey:        s.pk,
	}

	bytesToSign, err := s.enc.SignModeHandler().GetSignBytes(
		signing.SignMode_SIGN_MODE_DIRECT,
		signerData,
		builder.GetTx(),
	)
	if err != nil {
		return nil, fmt.Errorf("error getting sign bytes: %w", err)
	}

	signature, _, err := s.keys.SignByAddress(s.address, bytesToSign)
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

// QueryAccount fetches the account number and sequence number from the celestia-app node.
func QueryAccount(ctx context.Context, conn *grpc.ClientConn, encCfg encoding.Config, address string) (accNum uint64, seqNum uint64, err error) {
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
	return accNum, seqNum, nil
}
