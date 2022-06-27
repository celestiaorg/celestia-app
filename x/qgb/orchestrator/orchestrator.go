package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"

	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/rpc/client/http"
	"google.golang.org/grpc"
)

var (
	waitGroup sync.WaitGroup
)

type WorkerContext struct {
	logger tmlog.Logger

	// orch signing signing
	evmPrivateKey ecdsa.PrivateKey
	signer        *paytypes.KeyringSigner
	bridgeID      ethcmn.Hash

	// orch related
	orchestratorAddress sdk.AccAddress
	orchEthAddress      stakingtypes.EthAddress

	// query related
	querier       Querier
	tendermintRPC *http.HTTP
	qgbGrpc       *grpc.ClientConn
}

func NewWorkerContext(
	logger tmlog.Logger,
	keyringAccount,
	keyringPath,
	keyringBackend,
	tendermintRPC,
	celesGRPC,
	celestiaChainID string,
	evmPrivateKey ecdsa.PrivateKey,
	bridgeId ethcmn.Hash,
) *WorkerContext {
	// creates the signer
	//TODO: optionally ask for input for a password
	ring, err := keyring.New("orchestrator", keyringBackend, keyringPath, strings.NewReader(""))
	if err != nil {
		panic(err)
	}
	signer := paytypes.NewKeyringSigner(
		ring,
		keyringAccount,
		celestiaChainID,
	)

	orchEthAddr, err := stakingtypes.NewEthAddress(crypto.PubkeyToAddress(evmPrivateKey.PublicKey).Hex())
	if err != nil {
		panic(err)
	}

	querier, err := NewQuerier(celesGRPC, tendermintRPC, logger, MakeEncodingConfig())
	if err != nil {
		panic(err)
	}

	// TODO close these connections
	trpc, err := http.New(tendermintRPC, "/websocket")
	if err != nil {
		panic(err)
	}
	err = trpc.Start()
	if err != nil {
		panic(err)
	}

	cGRPC, err := grpc.Dial(celesGRPC, grpc.WithInsecure())
	if err != nil {
		panic(err)
	}

	return &WorkerContext{
		logger:              logger,
		signer:              signer,
		evmPrivateKey:       evmPrivateKey,
		bridgeID:            bridgeId,
		orchestratorAddress: signer.GetSignerInfo().GetAddress(),
		orchEthAddress:      *orchEthAddr,
		querier:             querier,
		tendermintRPC:       trpc,
		qgbGrpc:             cGRPC,
	}
}

type Worker struct {
	Ctx         context.Context
	Context     WorkerContext
	NoncesQueue chan uint64
}

func NewWorker(
	ctx context.Context,
	context WorkerContext,
	noncesQueue chan uint64,
) *Worker {
	return &Worker{
		Ctx:         ctx,
		NoncesQueue: noncesQueue,
		Context:     context,
	}
}

func (w Worker) Start() {
	for i := range w.NoncesQueue {
		if err := w.Process(i); err != nil {
			// re-enqueue any failed job
			// TODO: Implement exponential backoff or max retries for a block height.
			go func() {
				w.Context.logger.Error("re-enqueueing failed nonce", "nonce", i, "err", err)
				w.NoncesQueue <- i
			}()
		}
	}
}

func (w Worker) Process(nonce uint64) error {
	att, err := w.Context.querier.QueryAttestationByNonce(w.Ctx, nonce)
	if err != nil {
		return err
	}
	switch att.Type() {
	case types.ValsetRequestType:
		vs, ok := att.(*types.Valset)
		if !ok {
			return errors.Wrap(types.ErrAttestationNotValsetRequest, strconv.FormatUint(nonce, 10))
		}
		resp, err := w.Context.querier.QueryValsetConfirm(w.Ctx, nonce, w.Context.orchestratorAddress.String())
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("valset %d", nonce))
		}
		if resp != nil {
			// already signed
			return nil
		}
		err = w.processValsetEvent(w.Ctx, *vs)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("valset %d", nonce))
		}
		return nil
	case types.DataCommitmentRequestType:
		dc, ok := att.(*types.DataCommitment)
		if !ok {
			return errors.Wrap(types.ErrAttestationNotDataCommitmentRequest, strconv.FormatUint(nonce, 10))
		}
		resp, err := w.Context.querier.QueryDataCommitmentConfirm(w.Ctx, dc.EndBlock, dc.BeginBlock, w.Context.orchestratorAddress.String())
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("data commitment %d", nonce))
		}
		if resp != nil {
			// already signed
			return nil
		}
		err = w.processDataCommitmentEvent(w.Ctx, *dc)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("data commitment %d", nonce))
		}
		return nil
	default:
		return errors.Wrap(ErrUnknownAttestationType, strconv.FormatUint(nonce, 10))
	}
}

func (w Worker) processValsetEvent(ctx context.Context, valset types.Valset) error {
	signBytes, err := valset.SignBytes(w.Context.bridgeID)
	if err != nil {
		return err
	}

	signature, err := types.NewEthereumSignature(signBytes.Bytes(), &w.Context.evmPrivateKey)
	if err != nil {
		return err
	}

	// create and send the valset hash
	msg := types.NewMsgValsetConfirm(
		valset.Nonce,
		w.Context.orchEthAddress,
		w.Context.orchestratorAddress,
		ethcmn.Bytes2Hex(signature),
	)

	hash, err := w.broadcastTx(ctx, msg)
	if err != nil {
		return err
	}
	w.Context.logger.Info("signed Valset", "nonce", msg.Nonce, "txhash", hash)
	return nil
}

func (w Worker) processDataCommitmentEvent(
	ctx context.Context,
	dc types.DataCommitment,
) error {

	dcResp, err := w.Context.tendermintRPC.DataCommitment(
		ctx,
		fmt.Sprintf("block.height >= %d AND block.height <= %d",
			dc.BeginBlock,
			dc.EndBlock,
		),
	)
	if err != nil {
		return err
	}
	dataRootHash := types.DataCommitmentTupleRootSignBytes(w.Context.bridgeID, big.NewInt(int64(dc.Nonce)), dcResp.DataCommitment)
	dcSig, err := types.NewEthereumSignature(dataRootHash.Bytes(), &w.Context.evmPrivateKey)
	if err != nil {
		return err
	}

	msg := types.NewMsgDataCommitmentConfirm(
		dcResp.DataCommitment.String(),
		ethcmn.Bytes2Hex(dcSig),
		w.Context.orchestratorAddress,
		w.Context.orchEthAddress,
		dc.BeginBlock,
		dc.EndBlock,
		dc.Nonce,
	)

	hash, err := w.broadcastTx(ctx, msg)
	if err != nil {
		return err
	}
	w.Context.logger.Info(
		"signed commitment",
		"begin block",
		msg.BeginBlock,
		"end block",
		msg.EndBlock,
		"commitment",
		dcResp.DataCommitment,
		"txhash",
		hash,
	)

	return nil
}

func (w *Worker) broadcastTx(ctx context.Context, msg sdk.Msg) (string, error) {
	//bc.mutex.Lock()
	//defer bc.mutex.Unlock()
	err := w.Context.signer.QueryAccountNumber(ctx, w.Context.qgbGrpc)
	if err != nil {
		return "", err
	}

	builder := w.Context.signer.NewTxBuilder()
	// TODO make gas limit configurable
	builder.SetGasLimit(9999999999999)
	// TODO: update this api
	// via https://github.com/celestiaorg/celestia-app/pull/187/commits/37f96d9af30011736a3e6048bbb35bad6f5b795c
	tx, err := w.Context.signer.BuildSignedTx(builder, msg)
	if err != nil {
		return "", err
	}

	rawTx, err := w.Context.signer.EncodeTx(tx)
	if err != nil {
		return "", err
	}

	// TODO  check if we can move this outside of the paytypes
	resp, err := paytypes.BroadcastTx(ctx, w.Context.qgbGrpc, 1, rawTx)
	if err != nil {
		return "", err
	}

	if resp.TxResponse.Code != 0 {
		return "", errors.Wrap(ErrFailedBroadcast, resp.TxResponse.RawLog)
	}

	return resp.TxResponse.TxHash, nil
}
