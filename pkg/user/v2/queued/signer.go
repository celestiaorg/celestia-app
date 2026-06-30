package queued

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v9/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	blobtypes "github.com/celestiaorg/celestia-app/v9/x/blob/types"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
)

// txSigner produces wire-ready transaction bytes for a TxRequest. It hides
// SDK-, PFB-, and gas-estimation concerns from the worker.
//
// seq is the sequence queued requires the tx to be signed with. The signer forces
// the account to this sequence right before signing, so any sequence reset
// performed by the underlying gas-estimation path (which queued does not control)
// cannot desync the signer from queued's buffer. queued is the sole authority over
// sequence assignment; real on-chain mismatches are handled at submit time.
type txSigner interface {
	Sign(ctx context.Context, req *TxRequest, seq uint64) (txBytes []byte, txHash string, sequence uint64, err error)
}

// sdkTxSigner is the production txSigner. It serializes signing through
// the underlying v1 TxClient's mutex so the account's sequence counter
// is incremented atomically with the signature.
type sdkTxSigner struct {
	txClient    *user.TxClient
	accountName string
}

func newSDKTxSigner(txClient *user.TxClient, accountName string) *sdkTxSigner {
	return &sdkTxSigner{txClient: txClient, accountName: accountName}
}

func (s *sdkTxSigner) Sign(ctx context.Context, req *TxRequest, seq uint64) ([]byte, string, uint64, error) {
	s.txClient.Lock()
	defer s.txClient.Unlock()

	if err := s.txClient.CheckAccountLoaded(ctx, s.accountName); err != nil {
		return nil, "", 0, err
	}

	if req.Blobs != nil {
		return s.signPFB(ctx, req, seq)
	}
	return s.signRegular(ctx, req, seq)
}

func (s *sdkTxSigner) signPFB(ctx context.Context, req *TxRequest, seq uint64) ([]byte, string, uint64, error) {
	signer := s.txClient.Signer()
	acc, exists := signer.GetAccount(s.accountName)
	if !exists {
		return nil, "", 0, fmt.Errorf("account %s not found", s.accountName)
	}

	addr := acc.Address().String()
	msg, err := blobtypes.NewMsgPayForBlobs(addr, 0, req.Blobs...)
	if err != nil {
		return nil, "", 0, err
	}

	gasPrice, gasLimit, err := s.txClient.EstimateGasPriceAndUsage(ctx, []sdktypes.Msg{msg}, gasestimation.TxPriority_TX_PRIORITY_MEDIUM, req.Opts...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("estimating gas: %w", err)
	}
	fee := uint64(math.Ceil(gasPrice * float64(gasLimit)))
	opts := append([]user.TxOption{user.SetGasLimit(gasLimit), user.SetFee(fee)}, req.Opts...)

	// Gas estimation above may have reset the account's sequence (the v1
	// estimation path corrects sequence mismatches against the node's view).
	// Force it back to the sequence queued requires before signing for real, so
	// the signer never desyncs from queued's buffer.
	if err := signer.SetSequence(s.accountName, seq); err != nil {
		return nil, "", 0, fmt.Errorf("setting sequence: %w", err)
	}

	txBytes, seq, err := signer.CreatePayForBlobs(s.accountName, req.Blobs, opts...)
	if err != nil {
		return nil, "", 0, err
	}

	if err := signer.IncrementSequence(s.accountName); err != nil {
		return nil, "", 0, err
	}

	return txBytes, computeTxHash(txBytes), seq, nil
}

func (s *sdkTxSigner) signRegular(ctx context.Context, req *TxRequest, seq uint64) ([]byte, string, uint64, error) {
	signer := s.txClient.Signer()

	txBuilder, err := signer.TxBuilder(req.Msgs, req.Opts...)
	if err != nil {
		return nil, "", 0, err
	}

	hasUserSetFee := false
	for _, coin := range txBuilder.GetTx().GetFee() {
		if coin.Denom == appconsts.BondDenom {
			hasUserSetFee = true
			break
		}
	}

	gasLimit := txBuilder.GetTx().GetGas()
	if gasLimit == 0 {
		if !hasUserSetFee {
			txBuilder.SetFeeAmount(sdktypes.NewCoins(sdktypes.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1))))
		}
		gasLimit, err = s.txClient.EstimateGasForTx(ctx, txBuilder)
		if err != nil {
			return nil, "", 0, fmt.Errorf("gas estimation: %w", err)
		}
		txBuilder.SetGasLimit(gasLimit)
	}

	if !hasUserSetFee {
		fee := int64(math.Ceil(appconsts.DefaultMinGasPrice * float64(gasLimit)))
		txBuilder.SetFeeAmount(sdktypes.NewCoins(sdktypes.NewCoin(appconsts.BondDenom, sdkmath.NewInt(fee))))
	}

	// Force the sequence queued requires before signing for real: gas estimation
	// above may have reset it against the node's view (see signPFB).
	if err := signer.SetSequence(s.accountName, seq); err != nil {
		return nil, "", 0, fmt.Errorf("setting sequence: %w", err)
	}

	accountName, seq, err := signer.SignTransaction(txBuilder)
	if err != nil {
		return nil, "", 0, err
	}

	txBytes, err := signer.EncodeTx(txBuilder.GetTx())
	if err != nil {
		return nil, "", 0, err
	}

	if err := signer.IncrementSequence(accountName); err != nil {
		return nil, "", 0, err
	}

	return txBytes, computeTxHash(txBytes), seq, nil
}

// computeTxHash returns the hex-encoded SHA256 of tx bytes. For BlobTx
// envelopes it hashes the inner Tx, matching what the node sees.
func computeTxHash(txBytes []byte) string {
	blobTx, isBlobTx, err := blobtx.UnmarshalBlobTx(txBytes)
	if isBlobTx && err == nil {
		txBytes = blobTx.Tx
	}
	sum := sha256.Sum256(txBytes)
	return hex.EncodeToString(sum[:])
}
