package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktypestx "github.com/cosmos/cosmos-sdk/types/tx"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"google.golang.org/grpc"
	"math/big"
	"strconv"
	"sync"
	"time"
)

type Orchestrator struct {
	ctx    context.Context
	logger tmlog.Logger // maybe use a more general interface

	evmPrivateKey ecdsa.PrivateKey
	signer        *paytypes.KeyringSigner

	orchEthAddress stakingtypes.EthAddress
	noncesQueue    <-chan uint64
	retriesNumber  int

	querier     Querier
	broadcaster Broadcaster
}

func NewOrchestrator(
	ctx context.Context,
	logger tmlog.Logger,
	querier Querier,
	broadcaster Broadcaster,
	signer *paytypes.KeyringSigner,
	evmPrivateKey ecdsa.PrivateKey,
	noncesQueue <-chan uint64,
	retriesNumber int,
) *Orchestrator {
	orchEthAddr, err := stakingtypes.NewEthAddress(crypto.PubkeyToAddress(evmPrivateKey.PublicKey).Hex())
	if err != nil {
		panic(err)
	}

	return &Orchestrator{
		ctx:            ctx,
		logger:         logger,
		signer:         signer,
		evmPrivateKey:  evmPrivateKey,
		orchEthAddress: *orchEthAddr,
		querier:        querier,
		noncesQueue:    noncesQueue,
		retriesNumber:  retriesNumber,
		broadcaster:    broadcaster,
	}
}

func (orch Orchestrator) Start() {
	for i := range orch.noncesQueue {
		orch.logger.Debug("processing nonce", "nonce", i)
		if err := orch.Process(i); err != nil {
			orch.logger.Error("failed to process nonce, retrying...", "nonce", i, "err", err)
			if orch.Retry(i) != nil {
				panic(err)
			}
		}
	}
}

func (orch Orchestrator) Retry(nonce uint64) error {
	var err error
	for i := 0; i <= orch.retriesNumber; i++ {
		// We can implement some exponential backoff in here
		time.Sleep(10 * time.Second)
		orch.logger.Info("retrying", "nonce", nonce, "retry_number", i, "retries_left", orch.retriesNumber-i)
		err = orch.Process(nonce)
		if err == nil {
			orch.logger.Info("nonce processing succeeded", "nonce", nonce, "retries_number", i)
			return nil
		}
		orch.logger.Error("failed to process nonce", "nonce", nonce, "retry", i, "err", err)
	}
	return err
}

func (orch Orchestrator) Process(nonce uint64) error {
	att, err := orch.querier.QueryAttestationByNonce(orch.ctx, nonce)
	if err != nil {
		return err
	}
	switch att.Type() {
	case types.ValsetRequestType:
		vs, ok := att.(*types.Valset)
		if !ok {
			return errors.Wrap(types.ErrAttestationNotValsetRequest, strconv.FormatUint(nonce, 10))
		}
		resp, err := orch.querier.QueryValsetConfirm(orch.ctx, nonce, orch.signer.GetSignerInfo().GetAddress().String())
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("valset %d", nonce))
		}
		if resp != nil {
			orch.logger.Debug("already signed valset", "nonce", nonce, "signature", resp.Signature)
			return nil
		}
		err = orch.processValsetEvent(orch.ctx, *vs)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("valset %d", nonce))
		}
		return nil
	case types.DataCommitmentRequestType:
		dc, ok := att.(*types.DataCommitment)
		if !ok {
			return errors.Wrap(types.ErrAttestationNotDataCommitmentRequest, strconv.FormatUint(nonce, 10))
		}
		resp, err := orch.querier.QueryDataCommitmentConfirm(orch.ctx, dc.EndBlock, dc.BeginBlock, orch.signer.GetSignerInfo().GetAddress().String())
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("data commitment %d", nonce))
		}
		if resp != nil {
			orch.logger.Debug("already signed data commitment", "nonce", nonce, "begin_block", resp.BeginBlock, "end_block", resp.EndBlock, "commitment", resp.Commitment, "signature", resp.Signature)
			return nil
		}
		err = orch.processDataCommitmentEvent(orch.ctx, *dc)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("data commitment %d", nonce))
		}
		return nil
	default:
		return errors.Wrap(ErrUnknownAttestationType, strconv.FormatUint(nonce, 10))
	}
}

func (orch Orchestrator) processValsetEvent(ctx context.Context, valset types.Valset) error {
	signBytes, err := valset.SignBytes(types.BridgeId)
	if err != nil {
		return err
	}

	signature, err := types.NewEthereumSignature(signBytes.Bytes(), &orch.evmPrivateKey)
	if err != nil {
		return err
	}

	// create and send the valset hash
	msg := types.NewMsgValsetConfirm(
		valset.Nonce,
		orch.orchEthAddress,
		orch.signer.GetSignerInfo().GetAddress(),
		ethcmn.Bytes2Hex(signature),
	)

	hash, err := orch.broadcaster.BroadcastTx(ctx, msg)
	if err != nil {
		return err
	}
	orch.logger.Info("signed Valset", "nonce", msg.Nonce, "tx_hash", hash)
	return nil
}

func (orch Orchestrator) processDataCommitmentEvent(
	ctx context.Context,
	dc types.DataCommitment,
) error {
	commitment, err := orch.querier.QueryCommitment(
		ctx,
		fmt.Sprintf("block.height >= %d AND block.height <= %d",
			dc.BeginBlock,
			dc.EndBlock,
		),
	)
	if err != nil {
		return err
	}
	dataRootHash := types.DataCommitmentTupleRootSignBytes(types.BridgeId, big.NewInt(int64(dc.Nonce)), commitment)
	dcSig, err := types.NewEthereumSignature(dataRootHash.Bytes(), &orch.evmPrivateKey)
	if err != nil {
		return err
	}

	msg := types.NewMsgDataCommitmentConfirm(
		commitment.String(),
		ethcmn.Bytes2Hex(dcSig),
		orch.signer.GetSignerInfo().GetAddress(),
		orch.orchEthAddress,
		dc.BeginBlock,
		dc.EndBlock,
		dc.Nonce,
	)

	hash, err := orch.broadcaster.BroadcastTx(ctx, msg)
	if err != nil {
		return err
	}
	orch.logger.Info("signed commitment", "nonce", msg.Nonce, "begin_block", msg.BeginBlock, "end_block", msg.EndBlock, "commitment", commitment, "tx_hash", hash)
	return nil
}

var _ Broadcaster = &broadcaster{}

type Broadcaster interface {
	BroadcastTx(ctx context.Context, msg sdk.Msg) (string, error)
}

type broadcaster struct {
	mutex   *sync.Mutex
	signer  *paytypes.KeyringSigner
	qgbGrpc *grpc.ClientConn
}

func NewBroadcaster(qgbGrpcAddr string, signer *paytypes.KeyringSigner) (Broadcaster, error) {
	qgbGrpc, err := grpc.Dial(qgbGrpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return &broadcaster{
		mutex:   &sync.Mutex{}, // investigate if this is needed
		signer:  signer,
		qgbGrpc: qgbGrpc,
	}, nil
}

func (bc *broadcaster) BroadcastTx(ctx context.Context, msg sdk.Msg) (string, error) {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()
	err := bc.signer.QueryAccountNumber(ctx, bc.qgbGrpc)
	if err != nil {
		return "", err
	}

	builder := bc.signer.NewTxBuilder()
	// TODO make gas limit configurable
	builder.SetGasLimit(9999999999999)
	// TODO: update this api
	// via https://github.com/celestiaorg/celestia-app/pull/187/commits/37f96d9af30011736a3e6048bbb35bad6f5b795c
	tx, err := bc.signer.BuildSignedTx(builder, msg)
	if err != nil {
		return "", err
	}

	rawTx, err := bc.signer.EncodeTx(tx)
	if err != nil {
		return "", err
	}

	// TODO  check if we can move this outside of the paytypes
	resp, err := paytypes.BroadcastTx(ctx, bc.qgbGrpc, sdktypestx.BroadcastMode_BROADCAST_MODE_BLOCK, rawTx)
	if err != nil {
		return "", err
	}

	if resp.TxResponse.Code != 0 {
		return "", errors.Wrap(ErrFailedBroadcast, resp.TxResponse.RawLog)
	}

	return resp.TxResponse.TxHash, nil
}
