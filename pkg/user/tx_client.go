package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/celestiaorg/go-square/blob"
	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	abci "github.com/tendermint/tendermint/abci/types"
	"google.golang.org/grpc"

	"github.com/celestiaorg/celestia-app/v2/app/encoding"
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
	}
}

func WithDefaultAccount(name string) Option {
	return func(c *TxClient) {
		if _, err := c.signer.keys.Key(name); err != nil {
			panic(err)
		}
		c.defaultAccount = name
	}
}

// TxClient is an abstraction for building, signing, and broadcasting Celestia transactions
// It supports multiple accounts. If none is specified, it will
// try use the default account.
// TxClient is thread-safe.
type TxClient struct {
	mtx      sync.Mutex
	signer   *Signer
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
	signer *Signer,
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

	accounts := make([]*Account, len(records))
	for idx, record := range records {
		addr, err := record.GetAddress()
		if err != nil {
			return nil, err
		}
		accNum, seqNum, err := QueryAccount(ctx, conn, encCfg.InterfaceRegistry, addr)
		if err != nil {
			return nil, fmt.Errorf("querying account: %w", err)
		}

		accounts[idx] = NewAccount(record.Name, accNum, seqNum)
	}

	signer, err := NewSigner(keys, encCfg.TxConfig, chainID, appVersion, accounts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	return NewTxClient(signer, conn, encCfg.InterfaceRegistry, options...)
}

// SubmitPayForBlob forms a transaction from the provided blobs, signs it, and submits it to the chain.
// TxOptions may be provided to set the fee and gas limit.
func (s *TxClient) SubmitPayForBlob(ctx context.Context, blobs []*blob.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	resp, err := s.BroadcastPayForBlob(ctx, blobs, opts...)
	if err != nil {
		return resp, err
	}

	return s.ConfirmTx(ctx, resp.TxHash)
}

func (s *TxClient) SubmitPayForBlobsWithAccount(ctx context.Context, account string, blobs []*blob.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	resp, err := s.BroadcastPayForBlobWithAccount(ctx, account, blobs, opts...)
	if err != nil {
		return resp, err
	}

	return s.ConfirmTx(ctx, resp.TxHash)
}

// BroadcastPayForBlob signs and broadcasts a transaction to pay for blobs.
// It does not confirm that the transaction has been committed on chain.
func (s *TxClient) BroadcastPayForBlob(ctx context.Context, blobs []*blob.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	return s.BroadcastPayForBlobWithAccount(ctx, s.defaultAccount, blobs, opts...)
}

func (s *TxClient) BroadcastPayForBlobWithAccount(ctx context.Context, account string, blobs []*blob.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if err := s.checkAccountLoaded(ctx, account); err != nil {
		return nil, err
	}

	txBytes, seqNum, err := s.signer.CreatePayForBlobs(ctx, account, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return s.broadcastTx(ctx, txBytes, account, seqNum)
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

	tx, account, sequence, err := s.signer.SignTx(msgs, opts...)
	if err != nil {
		return nil, err
	}

	txBytes, err := s.signer.EncodeTx(tx)
	if err != nil {
		return nil, err
	}

	return s.broadcastTx(ctx, txBytes, account, sequence)
}

func (s *TxClient) broadcastTx(ctx context.Context, txBytes []byte, signer string, sequence uint64) (*sdktypes.TxResponse, error) {
	fmt.Println("broadcastTx called")
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
	fmt.Println("broadcasttx got here")
	if resp.TxResponse.Code != abci.CodeTypeOK {
		fmt.Println(resp.TxResponse, "TX RESPONSE")
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
	blobTx, isBlobTx := blob.UnmarshalBlobTx(txBytes)
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

	signer, sequence, err := s.signer.signTransaction(txBuilder)
	if err != nil {
		return nil, fmt.Errorf("resigning transaction: %w", err)
	}

	newTxBytes, err := s.signer.EncodeTx(txBuilder.GetTx())
	if err != nil {
		return nil, err
	}

	// rewrap the blob tx if it was originally a blob tx
	if isBlobTx {
		newTxBytes, err = blob.MarshalBlobTx(newTxBytes, blobTx.Blobs...)
		if err != nil {
			return nil, err
		}
	}

	return s.broadcastTx(ctx, newTxBytes, signer, sequence)
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
func (s *TxClient) Account(name string) (*Account, bool) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	acc, exists := s.signer.accounts[name]
	return acc.Copy(), exists
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
	accNum, sequence, err := QueryAccount(ctx, s.grpc, s.registry, addr)
	if err != nil {
		return fmt.Errorf("querying account %s: %w", account, err)
	}
	return s.signer.AddAccount(NewAccount(account, accNum, sequence))
}

// Signer exposes the tx clients underlying signer
func (s *TxClient) Signer() *Signer {
	return s.signer
}
