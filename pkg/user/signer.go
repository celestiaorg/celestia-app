package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/app/encoding"
	apperrors "github.com/celestiaorg/celestia-app/app/errors"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/go-square/blob"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
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
	// FIXME: the signer is currently incapable of detecting an appversion
	// change and could produce incorrect PFBs if it the network is at an
	// appVersion that the signer does not support
	appVersion uint64

	mtx                    sync.RWMutex
	pollTime               time.Duration
	earliestNonce          uint64
	latestNonce            uint64
	outboundTxs            map[uint64][]byte
	reverseHashSequenceMap map[string]uint64
}

// NewSigner returns a new signer using the provided keyring
func NewSigner(
	keys keyring.Keyring,
	conn *grpc.ClientConn,
	address sdktypes.AccAddress,
	enc client.TxConfig,
	chainID string,
	accountNumber, sequence,
	appVersion uint64,
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
		keys:          keys,
		address:       address,
		grpc:          conn,
		enc:           enc,
		pk:            pk,
		chainID:       chainID,
		accountNumber: accountNumber,
		appVersion:    appVersion,
		earliestNonce: sequence,
		latestNonce:   sequence,
		pollTime:      DefaultPollTime,
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
	resp, err := tmservice.NewServiceClient(conn).GetLatestBlock(
		ctx,
		&tmservice.GetLatestBlockRequest{},
	)
	if err != nil {
		return nil, err
	}

	chainID := resp.SdkBlock.Header.ChainID
	appVersion := resp.SdkBlock.Header.Version.App
	accNum, seqNum, err := QueryAccount(ctx, conn, encCfg, address.String())
	if err != nil {
		return nil, err
	}

	return NewSigner(keys, conn, address, encCfg.TxConfig, chainID, accNum, seqNum, appVersion)
}

// SubmitTx forms a transaction from the provided messages, signs it, and submits it to the chain. TxOptions
// may be provided to set the fee and gas limit.
func (s *Signer) SubmitTx(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (*sdktypes.TxResponse, error) {
	tx, err := s.CreateTx(msgs, opts...)
	if err != nil {
		return nil, err
	}

	resp, err := s.BroadcastTx(ctx, tx)
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
	txBytes, seqNum, err := s.createPayForBlobs(blobs, opts...)
	if err != nil {
		return nil, err
	}

	resp, err := s.broadcastTx(ctx, txBytes, seqNum)
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
func (s *Signer) CreateTx(msgs []sdktypes.Msg, opts ...TxOption) (authsigning.Tx, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	return s.createTx(msgs, opts...)
}

func (s *Signer) createTx(msgs []sdktypes.Msg, opts ...TxOption) (authsigning.Tx, error) {
	txBuilder := s.txBuilder(opts...)
	if err := txBuilder.SetMsgs(msgs...); err != nil {
		return nil, err
	}

	if err := s.signTransaction(txBuilder, s.getAndIncrementSequence()); err != nil {
		return nil, err
	}

	return txBuilder.GetTx(), nil
}

func (s *Signer) CreatePayForBlob(blobs []*blob.Blob, opts ...TxOption) ([]byte, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	blobTx, _, err := s.createPayForBlobs(blobs)
	return blobTx, err
}

func (s *Signer) createPayForBlobs(blobs []*blob.Blob, opts ...TxOption) ([]byte, uint64, error) {
	msg, err := blobtypes.NewMsgPayForBlobs(s.address.String(), s.appVersion, blobs...)
	if err != nil {
		return nil, 0, err
	}

	tx, err := s.createTx([]sdktypes.Msg{msg}, opts...)
	if err != nil {
		return nil, 0, err
	}

	seqNum, err := getSequenceNumber(tx)
	if err != nil {
		panic(err)
	}

	txBytes, err := s.EncodeTx(tx)
	if err != nil {
		return nil, 0, err
	}

	blobTx, err := blob.MarshalBlobTx(txBytes, blobs...)
	return blobTx, seqNum, err
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

// BroadcastTx submits the provided transaction bytes to the chain and returns the response.
func (s *Signer) BroadcastTx(ctx context.Context, tx authsigning.Tx) (*sdktypes.TxResponse, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	txBytes, err := s.EncodeTx(tx)
	if err != nil {
		return nil, err
	}
	sequence, err := getSequenceNumber(tx)
	if err != nil {
		return nil, err
	}
	return s.broadcastTx(ctx, txBytes, sequence)
}

// CONTRACT: assumes the caller has the lock
func (s *Signer) broadcastTx(ctx context.Context, txBytes []byte, sequence uint64) (*sdktypes.TxResponse, error) {
	if _, exists := s.outboundTxs[sequence]; exists {
		// there is already a pending tx with that sequence number. Update it to the latest
		s.retryBroadcastingTx(ctx, txBytes, s.latestNonce+1)
	}
	// update the map recording all pending transactions
	s.outboundTxs[sequence] = txBytes
	if s.latestNonce < sequence {
		s.latestNonce = sequence
	} else if sequence < s.earliestNonce {
		s.earliestNonce = sequence
	}

	txClient := sdktx.NewServiceClient(s.grpc)
	resp, err := txClient.BroadcastTx(
		ctx,
		&sdktx.BroadcastTxRequest{
			Mode:    sdktx.BroadcastMode_BROADCAST_MODE_SYNC,
			TxBytes: txBytes,
		},
	)
	if err != nil {
		// check if the transaction failed due to a nonce mismatch
		if apperrors.IsNonceMismatch(err) {
			// extract what the lastCommittedNonce on chain is
			latestCommittedNonce, err := apperrors.ParseNonceMismatch(err)
			if err != nil {
				return nil, fmt.Errorf("parsing nonce mismatch upon retry: %w", err)
			}
			nextSequence := latestCommittedNonce + 1
			delete(s.outboundTxs, sequence)
			if nextSequence < s.earliestNonce || nextSequence > s.latestNonce {
				return s.retryBroadcastingTx(ctx, txBytes, nextSequence)
			} else {

			}
		}
		return nil, err
	}
	s.reverseHashSequenceMap[string(resp.TxResponse.TxHash)] = sequence
	return resp.TxResponse, nil
}

func (s *Signer) retryBroadcastingTx(ctx context.Context, txBytes []byte, newSequenceNumber uint64) (*sdktypes.TxResponse, error) {
	blobTx, isBlobTx := blob.UnmarshalBlobTx(txBytes)
	if isBlobTx {
		txBytes = blobTx.Tx
	}
	tx, err := s.DecodeTx(txBytes)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

// ConfirmTx periodically pings the provided node for the commitment of a transaction by its
// hash. It will continually loop until the context is cancelled, the tx is found or an error
// is encountered.
func (s *Signer) ConfirmTx(ctx context.Context, txHash string) (*sdktypes.TxResponse, error) {
	txClient := sdktx.NewServiceClient(s.grpc)

	pollTicker := time.NewTicker(s.getPollTime())
	defer pollTicker.Stop()

	for {
		resp, err := txClient.GetTx(ctx, &sdktx.GetTxRequest{Hash: txHash})
		if err == nil {
			if resp.TxResponse.Code != 0 {
				s.confirmCommittedTx(txHash, false)
				return resp.TxResponse, fmt.Errorf("tx failed with code %d: %s", resp.TxResponse.Code, resp.TxResponse.RawLog)
			}
			s.confirmCommittedTx(txHash, true)
			return resp.TxResponse, nil
		}
		// FIXME: this is a relatively brittle of working out whether to retry or not. The tx might be not found for other
		// reasons. It may have been removed from the mempool at a later point. We should build an endpoint that gives the
		// signer more information on the status of their transaction and then update the logic here
		if !strings.Contains(err.Error(), "not found") {
			return &sdktypes.TxResponse{}, err
		}

		// Wait for the next round.
		select {
		case <-ctx.Done():
			return &sdktypes.TxResponse{}, ctx.Err()
		case <-pollTicker.C:
		}
	}
}

func (s *Signer) EstimateGas(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (uint64, error) {
	txBuilder := s.txBuilder(opts...)
	if err := txBuilder.SetMsgs(msgs...); err != nil {
		return 0, err
	}

	if err := s.signTransaction(txBuilder, s.Sequence()); err != nil {
		return 0, err
	}

	txBytes, err := s.enc.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return 0, err
	}

	resp, err := sdktx.NewServiceClient(s.grpc).Simulate(ctx, &sdktx.SimulateRequest{
		TxBytes: txBytes,
	})
	if err != nil {
		return 0, err
	}

	return resp.GasInfo.GasUsed, nil
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

func (s *Signer) getPollTime() time.Duration {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.pollTime
}

// PubKey returns the public key of the signer
func (s *Signer) PubKey() cryptotypes.PubKey {
	return s.pk
}

// Sequence returns the last signed sequence number of the signer
func (s *Signer) Sequence() uint64 {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.latestNonce
}

// getAndIncrementSequence gets the latest signed sequence and increments the local sequence number
func (s *Signer) getAndIncrementSequence() uint64 {
	defer func() { s.latestNonce++ }()
	return s.latestNonce
}

// ForceSetSequence manually overrides the current sequence number. Be careful when
// invoking this as it may cause the transactions to reject the sequence if
// it doesn't match the one in state
func (s *Signer) ForceSetSequence(seq uint64) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.latestNonce = seq
}

// Keyring exposes the signers underlying keyring
func (s *Signer) Keyring() keyring.Keyring {
	return s.keys
}

func (s *Signer) signTransaction(builder client.TxBuilder, sequence uint64) error {
	signers := builder.GetTx().GetSigners()
	if len(signers) != 1 {
		return fmt.Errorf("expected 1 signer, got %d", len(signers))
	}

	if !s.address.Equals(signers[0]) {
		return fmt.Errorf("expected signer %s, got %s", s.address.String(), signers[0].String())
	}

	// To ensure we have the correct bytes to sign over we produce
	// a dry run of the signing data
	err := builder.SetSignatures(s.getSignatureV2(sequence, nil))
	if err != nil {
		return fmt.Errorf("error setting draft signatures: %w", err)
	}

	// now we can use the data to produce the signature from the signer
	signature, err := s.createSignature(builder, sequence)
	if err != nil {
		return fmt.Errorf("error creating signature: %w", err)
	}

	err = builder.SetSignatures(s.getSignatureV2(sequence, signature))
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

func (s *Signer) confirmCommittedTx(txHash string, success bool) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	txNonce, exists := s.reverseHashSequenceMap[txHash]
	if !exists {
		return
	}
	if success && txNonce == s.earliestNonce {
		s.earliestNonce++
		for nonce := s.earliestNonce; nonce <= s.latestNonce; nonce++ {
			if _, exists := s.outboundTxs[nonce]; !exists {
				s.earliestNonce++
			}
		}
	}
	// TODO: what about other pending transactions that are now rejected because of a nonce
	// mismatch. We need to track how they fail and possibly resubmit them
	delete(s.reverseHashSequenceMap, txHash)
	delete(s.outboundTxs, txNonce)
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

func (s *Signer) getSignatureV2(sequence uint64, signature []byte) signing.SignatureV2 {
	sigV2 := signing.SignatureV2{
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: signature,
		},
		Sequence: sequence,
	}
	if sequence == 0 {
		sigV2.PubKey = s.pk
	}
	return sigV2
}

func getSequenceNumber(tx authsigning.Tx) (uint64, error) {
	sigs, err := tx.GetSignaturesV2()
	if err != nil {
		return 0, err
	}
	if len(sigs) > 1 {
		return 0, fmt.Errorf("only a signle signature is supported, got %d", len(sigs))
	}

	return sigs[0].Sequence, nil
}
