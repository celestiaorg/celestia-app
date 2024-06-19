package user

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmtypes "github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"

	"github.com/celestiaorg/celestia-app/app/encoding"
	apperrors "github.com/celestiaorg/celestia-app/app/errors"
)

const (
	DefaultPollTime              = 3 * time.Second
	DefaultGasMultiplier float64 = 1.1
)

type Option func(s *TxClient)

// WithGasMultiplier is a functional option allows to configure the gas multiplier.
func WithGasMultiplier(multiplier float64) Option {
	return func(c *TxClient) {
		c.gasMultiplier = multiplier
	}
}

func WithPollTime(time time.Duration) Option {
	return func(c *TxClient) {
		c.pollTime = time
	}
}

func WithDefaultAddress(address sdktypes.AccAddress) Option {
	return func(c *TxClient) {
		record, err := c.signer.keys.KeyByAddress(address)
		if err != nil {
			panic(err)
		}
		c.defaultAccount = record.Name
		c.defaultAddress = address
	}
}

func WithDefaultAccount(name string) Option {
	return func(c *TxClient) {
		rec, err := c.signer.keys.Key(name)
		if err != nil {
			panic(err)
		}
		addr, err := rec.GetAddress()
		if err != nil {
			panic(err)
		}
		c.defaultAccount = name
		c.defaultAddress = addr
	}
}

// TxClient is an abstraction for building, signing, and broadcasting Celestia transactions
// It supports multiple accounts. If none is specified, it will
// try use the default account.
// TxClient is thread-safe.
type TxClient struct {
	mtx      sync.Mutex
	signer   *TxSigner
	registry codectypes.InterfaceRegistry
	grpc     *grpc.ClientConn
	// how often to poll the network for confirmation of a transaction
	pollTime time.Duration
	// gasMultiplier is used to increase gas limit as it is sometimes underestimated
	gasMultiplier  float64
	defaultAccount string
	defaultAddress sdktypes.AccAddress
}

// NewTxClient returns a new signer using the provided keyring
func NewTxClient(
	signer *TxSigner,
	conn *grpc.ClientConn,
	registry codectypes.InterfaceRegistry,
	options ...Option,
) (*TxClient, error) {
	records, err := signer.keys.List()
	if err != nil {
		return nil, fmt.Errorf("retrieving keys: %w", err)
	}

	if len(records) == 0 {
		return nil, errors.New("signer must have at least one key")
	}

	addr, err := records[0].GetAddress()
	if err != nil {
		return nil, err
	}

	txClient := &TxClient{
		signer:         signer,
		registry:       registry,
		grpc:           conn,
		pollTime:       DefaultPollTime,
		gasMultiplier:  DefaultGasMultiplier,
		defaultAccount: records[0].Name,
		defaultAddress: addr,
	}

	for _, opt := range options {
		opt(txClient)
	}

	return txClient, nil
}

// SetupTxClient uses the underlying grpc connection to populate the chainID, accountNumber and sequence number of all
// the accounts in the keyring.
func SetupTxClient(
	ctx context.Context,
	keys keyring.Keyring,
	conn *grpc.ClientConn,
	encCfg encoding.Config,
	options ...Option,
) (*TxClient, error) {
	resp, err := tmservice.NewServiceClient(conn).GetLatestBlock(
		ctx,
		&tmservice.GetLatestBlockRequest{},
	)
	if err != nil {
		return nil, err
	}

	chainID := resp.SdkBlock.Header.ChainID
	appVersion := resp.SdkBlock.Header.Version.App

	records, err := keys.List()
	if err != nil {
		return nil, err
	}

	accounts := make([]*Account, 0, len(records))
	for _, record := range records {
		addr, err := record.GetAddress()
		if err != nil {
			return nil, err
		}
		accNum, seqNum, err := QueryAccountInfo(ctx, conn, encCfg.InterfaceRegistry, addr)
		if err != nil {
			// skip over the accounts that don't exist in state
			continue
		}

		accounts = append(accounts, NewAccount(record.Name, accNum, seqNum))
	}

	signer, err := NewTxSigner(keys, encCfg.TxConfig, chainID, appVersion, accounts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	return NewTxClient(signer, conn, encCfg.InterfaceRegistry, options...)
}

// SubmitPayForBlob forms a transaction from the provided blobs, signs it, and submits it to the chain.
// TxOptions may be provided to set the fee and gas limit.
func (s *TxClient) SubmitPayForBlob(ctx context.Context, blobs []*tmproto.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	resp, err := s.BroadcastPayForBlob(ctx, blobs, opts...)
	if err != nil {
		return resp, err
	}

	return s.ConfirmTx(ctx, resp.TxHash)
}

func (s *TxClient) SubmitPayForBlobsWithAccount(ctx context.Context, account string, blobs []*tmproto.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	resp, err := s.BroadcastPayForBlobWithAccount(ctx, account, blobs, opts...)
	if err != nil {
		return resp, err
	}

	return s.ConfirmTx(ctx, resp.TxHash)
}

// BroadcastPayForBlob signs and broadcasts a transaction to pay for blobs.
// It does not confirm that the transaction has been committed on chain.
func (s *TxClient) BroadcastPayForBlob(ctx context.Context, blobs []*tmproto.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	return s.BroadcastPayForBlobWithAccount(ctx, s.defaultAccount, blobs, opts...)
}

func (s *TxClient) BroadcastPayForBlobWithAccount(ctx context.Context, account string, blobs []*tmproto.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if err := s.checkAccountLoaded(ctx, account); err != nil {
		return nil, err
	}

	txBytes, _, err := s.signer.CreatePayForBlobs(account, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return s.broadcastTx(ctx, txBytes, account)
}

// SubmitTx forms a transaction from the provided messages, signs it, and submits it to the chain. TxOptions
// may be provided to set the fee and gas limit.
func (s *TxClient) SubmitTx(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (*sdktypes.TxResponse, error) {
	resp, err := s.BroadcastTx(ctx, msgs, opts...)
	if err != nil {
		return resp, err
	}

	return s.ConfirmTx(ctx, resp.TxHash)
}

func (s *TxClient) BroadcastTx(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (*sdktypes.TxResponse, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	account, err := s.getAccountNameFromMsgs(msgs)
	if err != nil {
		return nil, err
	}

	if err := s.checkAccountLoaded(ctx, account); err != nil {
		return nil, err
	}

	tx, account, _, err := s.signer.SignTx(msgs, opts...)
	if err != nil {
		return nil, err
	}

	txBytes, err := s.signer.EncodeTx(tx)
	if err != nil {
		return nil, err
	}

	return s.broadcastTx(ctx, txBytes, account)
}

func (s *TxClient) broadcastTx(ctx context.Context, txBytes []byte, signer string) (*sdktypes.TxResponse, error) {
	txClient := sdktx.NewServiceClient(s.grpc)
	resp, err := txClient.BroadcastTx(
		ctx,
		&sdktx.BroadcastTxRequest{
			Mode:    sdktx.BroadcastMode_BROADCAST_MODE_SYNC,
			TxBytes: txBytes,
		},
	)
	if err != nil {
		return nil, err
	}
	if resp.TxResponse.Code != abci.CodeTypeOK {
		if apperrors.IsNonceMismatchCode(resp.TxResponse.Code) {
			// query the account to update the sequence number on-chain for the account
			_, seqNum, err := QueryAccountInfo(ctx, s.grpc, s.registry, s.signer.accounts[signer].address)
			if err != nil {
				return nil, fmt.Errorf("querying account for new sequence number: %w\noriginal tx response: %s", err, resp.TxResponse.RawLog)
			}
			if err := s.signer.SetSequence(signer, seqNum); err != nil {
				return nil, fmt.Errorf("setting sequence: %w", err)
			}
			return s.retryBroadcastingTx(ctx, txBytes)
		}
		return resp.TxResponse, fmt.Errorf("tx failed with code %d: %s", resp.TxResponse.Code, resp.TxResponse.RawLog)
	}

	// after the transaction has been submitted, we can increment the
	// sequence of the signer
	if err := s.signer.IncrementSequence(signer); err != nil {
		return nil, fmt.Errorf("increment sequencing: %w", err)
	}
	return resp.TxResponse, nil
}

// retryBroadcastingTx creates a new transaction by copying over an existing transaction but creates a new signature with the
// new sequence number. It then calls `broadcastTx` and attempts to submit the transaction
func (s *TxClient) retryBroadcastingTx(ctx context.Context, txBytes []byte) (*sdktypes.TxResponse, error) {
	blobTx, isBlobTx := tmtypes.UnmarshalBlobTx(txBytes)
	if isBlobTx {
		txBytes = blobTx.Tx
	}
	tx, err := s.signer.DecodeTx(txBytes)
	if err != nil {
		return nil, err
	}
	txBuilder := s.signer.txBuilder()
	if err := txBuilder.SetMsgs(tx.GetMsgs()...); err != nil {
		return nil, err
	}
	if granter := tx.FeeGranter(); granter != nil {
		txBuilder.SetFeeGranter(granter)
	}
	if payer := tx.FeePayer(); payer != nil {
		txBuilder.SetFeePayer(payer)
	}
	if memo := tx.GetMemo(); memo != "" {
		txBuilder.SetMemo(memo)
	}
	if fee := tx.GetFee(); fee != nil {
		txBuilder.SetFeeAmount(fee)
	}
	if gas := tx.GetGas(); gas > 0 {
		txBuilder.SetGasLimit(gas)
	}

	signer, _, err := s.signer.signTransaction(txBuilder)
	if err != nil {
		return nil, fmt.Errorf("resigning transaction: %w", err)
	}

	newTxBytes, err := s.signer.EncodeTx(txBuilder.GetTx())
	if err != nil {
		return nil, err
	}

	// rewrap the blob tx if it was originally a blob tx
	if isBlobTx {
		newTxBytes, err = tmtypes.MarshalBlobTx(newTxBytes, blobTx.Blobs...)
		if err != nil {
			return nil, err
		}
	}

	return s.broadcastTx(ctx, newTxBytes, signer)
}

// ConfirmTx periodically pings the provided node for the commitment of a transaction by its
// hash. It will continually loop until the context is cancelled, the tx is found or an error
// is encountered.
func (s *TxClient) ConfirmTx(ctx context.Context, txHash string) (*sdktypes.TxResponse, error) {
	txClient := sdktx.NewServiceClient(s.grpc)

	pollTicker := time.NewTicker(s.pollTime)
	defer pollTicker.Stop()

	for {
		resp, err := txClient.GetTx(ctx, &sdktx.GetTxRequest{Hash: txHash})
		if err == nil {
			if resp.TxResponse.Code != 0 {
				return resp.TxResponse, fmt.Errorf("tx was included but failed with code %d: %s", resp.TxResponse.Code, resp.TxResponse.RawLog)
			}
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

func (s *TxClient) EstimateGas(ctx context.Context, msgs []sdktypes.Msg, opts ...TxOption) (uint64, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	txBuilder := s.signer.txBuilder(opts...)
	if err := txBuilder.SetMsgs(msgs...); err != nil {
		return 0, err
	}

	_, _, err := s.signer.signTransaction(txBuilder)
	if err != nil {
		return 0, err
	}

	txBytes, err := s.signer.EncodeTx(txBuilder.GetTx())
	if err != nil {
		return 0, err
	}

	resp, err := sdktx.NewServiceClient(s.grpc).Simulate(ctx, &sdktx.SimulateRequest{
		TxBytes: txBytes,
	})
	if err != nil {
		return 0, err
	}

	gasLimit := uint64(float64(resp.GasInfo.GasUsed) * s.gasMultiplier)
	return gasLimit, nil
}

// Account returns an account of the signer from the key name. Also returns a bool if the
// account exists.
// Thread-safe
func (s *TxClient) Account(name string) (*Account, bool) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	acc, exists := s.signer.accounts[name]
	if !exists {
		return nil, false
	}
	return acc.Copy(), true
}

func (s *TxClient) AccountByAddress(address sdktypes.AccAddress) *Account {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.signer.AccountByAddress(address)
}

func (s *TxClient) DefaultAddress() sdktypes.AccAddress {
	return s.defaultAddress
}

func (s *TxClient) DefaultAccountName() string { return s.defaultAccount }

func (s *TxClient) checkAccountLoaded(ctx context.Context, account string) error {
	if _, exists := s.signer.accounts[account]; exists {
		return nil
	}
	record, err := s.signer.keys.Key(account)
	if err != nil {
		return fmt.Errorf("trying to find account %s on keyring: %w", account, err)
	}
	addr, err := record.GetAddress()
	if err != nil {
		return fmt.Errorf("retrieving address from keyring: %w", err)
	}
	accNum, sequence, err := QueryAccountInfo(ctx, s.grpc, s.registry, addr)
	if err != nil {
		return fmt.Errorf("querying account %s: %w", account, err)
	}
	return s.signer.AddAccount(NewAccount(account, accNum, sequence))
}

func (s *TxClient) getAccountNameFromMsgs(msgs []sdktypes.Msg) (string, error) {
	var addr sdktypes.AccAddress
	for _, msg := range msgs {
		signers := msg.GetSigners()
		if len(signers) != 1 {
			return "", fmt.Errorf("only one signer per transaction supported, got %d", len(signers))
		}
		if addr == nil {
			addr = signers[0]
		}
		if !bytes.Equal(addr, signers[0]) {
			return "", errors.New("not supported: got two different signers across multiple messages")
		}
	}
	record, err := s.signer.keys.KeyByAddress(addr)
	if err != nil {
		return "", err
	}
	return record.Name, nil
}

// Signer exposes the tx clients underlying signer
func (s *TxClient) Signer() *TxSigner {
	return s.signer
}
