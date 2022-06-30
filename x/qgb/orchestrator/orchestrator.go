package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
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
	"math/big"
	"strconv"
	"strings"
	"time"
)

type Orchestrator struct {
	ctx    context.Context
	logger tmlog.Logger

	// orch signing signing
	evmPrivateKey ecdsa.PrivateKey
	signer        *paytypes.KeyringSigner
	bridgeID      ethcmn.Hash

	// orch related
	orchestratorAddress sdk.AccAddress
	orchEthAddress      stakingtypes.EthAddress
	noncesQueue         <-chan uint64
	retriesNumber       int

	// query related
	querier       Querier
	tendermintRPC *http.HTTP
	qgbGrpc       *grpc.ClientConn
}

func NewOrchestrator(
	ctx context.Context,
	logger tmlog.Logger,
	keyringAccount,
	keyringPath,
	keyringBackend,
	tendermintRPC,
	celesGRPC,
	celestiaChainID string,
	evmPrivateKey ecdsa.PrivateKey,
	bridgeId ethcmn.Hash,
	noncesQueue <-chan uint64,
	retriesNumber int,
) *Orchestrator {
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

	return &Orchestrator{
		ctx:                 ctx,
		logger:              logger,
		signer:              signer,
		evmPrivateKey:       evmPrivateKey,
		bridgeID:            bridgeId,
		orchestratorAddress: signer.GetSignerInfo().GetAddress(),
		orchEthAddress:      *orchEthAddr,
		querier:             querier,
		tendermintRPC:       trpc,
		qgbGrpc:             cGRPC,
		noncesQueue:         noncesQueue,
		retriesNumber:       retriesNumber,
	}
}

func (orch Orchestrator) Stop() {
	err := orch.tendermintRPC.Stop()
	if err != nil {
		panic(err)
	}
	err = orch.qgbGrpc.Close()
	if err != nil {
		panic(err)
	}
}

func (orch Orchestrator) Start() {
	for i := range orch.noncesQueue {
		orch.logger.Info("processing nonce", "nonce", i)
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
		resp, err := orch.querier.QueryValsetConfirm(orch.ctx, nonce, orch.orchestratorAddress.String())
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("valset %d", nonce))
		}
		if resp != nil {
			orch.logger.Info("already signed valset", "nonce", nonce, "signature", resp.Signature)
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
		resp, err := orch.querier.QueryDataCommitmentConfirm(orch.ctx, dc.EndBlock, dc.BeginBlock, orch.orchestratorAddress.String())
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("data commitment %d", nonce))
		}
		if resp != nil {
			orch.logger.Info("already signed data commitment", "nonce", nonce, "begin_block", resp.BeginBlock, "end_block", resp.EndBlock, "commitment", resp.Commitment, "signature", resp.Signature)
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
	signBytes, err := valset.SignBytes(orch.bridgeID)
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
		orch.orchestratorAddress,
		ethcmn.Bytes2Hex(signature),
	)

	hash, err := orch.broadcastTx(ctx, msg)
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

	dcResp, err := orch.tendermintRPC.DataCommitment(
		ctx,
		fmt.Sprintf("block.height >= %d AND block.height <= %d",
			dc.BeginBlock,
			dc.EndBlock,
		),
	)
	if err != nil {
		return err
	}
	dataRootHash := types.DataCommitmentTupleRootSignBytes(orch.bridgeID, big.NewInt(int64(dc.Nonce)), dcResp.DataCommitment)
	dcSig, err := types.NewEthereumSignature(dataRootHash.Bytes(), &orch.evmPrivateKey)
	if err != nil {
		return err
	}

	msg := types.NewMsgDataCommitmentConfirm(
		dcResp.DataCommitment.String(),
		ethcmn.Bytes2Hex(dcSig),
		orch.orchestratorAddress,
		orch.orchEthAddress,
		dc.BeginBlock,
		dc.EndBlock,
		dc.Nonce,
	)

	hash, err := orch.broadcastTx(ctx, msg)
	if err != nil {
		return err
	}
	orch.logger.Info("signed commitment", "nonce", msg.Nonce, "begin_block", msg.BeginBlock, "end_block", msg.EndBlock, "commitment", dcResp.DataCommitment, "tx_hash", hash)
	return nil
}

func (orch Orchestrator) broadcastTx(ctx context.Context, msg sdk.Msg) (string, error) {
	//bc.mutex.Lock()
	//defer bc.mutex.Unlock()
	err := orch.signer.QueryAccountNumber(ctx, orch.qgbGrpc)
	if err != nil {
		return "", err
	}

	builder := orch.signer.NewTxBuilder()
	// TODO make gas limit configurable
	builder.SetGasLimit(9999999999999)
	// TODO: update this api
	// via https://github.com/celestiaorg/celestia-app/pull/187/commits/37f96d9af30011736a3e6048bbb35bad6f5b795c
	tx, err := orch.signer.BuildSignedTx(builder, msg)
	if err != nil {
		return "", err
	}

	rawTx, err := orch.signer.EncodeTx(tx)
	if err != nil {
		return "", err
	}

	// TODO  check if we can move this outside of the paytypes
	resp, err := paytypes.BroadcastTx(ctx, orch.qgbGrpc, 1, rawTx)
	if err != nil {
		return "", err
	}

	if resp.TxResponse.Code != 0 {
		return "", errors.Wrap(ErrFailedBroadcast, resp.TxResponse.RawLog)
	}

	return resp.TxResponse.TxHash, nil
}
