package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"syscall"

	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/celestiaorg/quantum-gravity-bridge/orchestrator/ethereum/keystore"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcmn "github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/tendermint/tendermint/rpc/client/http"
	coretypes "github.com/tendermint/tendermint/types"
	"golang.org/x/term"
	"google.golang.org/grpc"
)

type orchClient struct {
	// administrativa
	ctx    context.Context
	cancel context.CancelFunc
	wg     *sync.WaitGroup

	// RPC
	tendermintRPC *http.HTTP
	qgbRPC        *grpc.ClientConn

	// orchestrator signing
	singerFn         bind.SignerFn
	personalSignerFn keystore.PersonalSignFn
	orchestratorAddr ethcmn.Address
	bridgeID         ethcmn.Hash

	// celestia related signing
	signer *paytypes.KeyringSigner
}

func newOrchClient(
	ctx context.Context,
	logger zerolog.Logger,
	appSigner *paytypes.KeyringSigner,
	bridgeID ethcmn.Hash,
	chainID uint64,
	tendermintRPC,
	qgbRPC,
	ethPrivKey string,
) (*orchClient, error) {
	ctx, cancel := context.WithCancel(ctx)

	trpc, err := http.New(tendermintRPC, "/websocket")
	if err != nil {
		cancel()
		return nil, err
	}

	qgbGRPC, err := grpc.Dial(qgbRPC, grpc.WithInsecure())
	if err != nil {
		cancel()
		return nil, err
	}

	orchAddr, sfn, psfn, err := initEthSigners(chainID, ethPrivKey)
	if err != nil {
		cancel()
		return nil, err
	}

	return &orchClient{
		tendermintRPC:    trpc,
		singerFn:         sfn,
		personalSignerFn: psfn,
		ctx:              ctx,
		cancel:           cancel,
		qgbRPC:           qgbGRPC,
		wg:               &sync.WaitGroup{},
		orchestratorAddr: orchAddr,
		signer:           appSigner,
		bridgeID:         bridgeID,
	}, nil
}

func (oc *orchClient) start() {
	err := oc.tendermintRPC.Start()
	if err != nil {
		panic(err)
	}
}

func (oc *orchClient) stop() {
	err := oc.tendermintRPC.Stop()
	if err != nil {
		panic(err)
	}

	err = oc.qgbRPC.Close()
	if err != nil {
		panic(err)
	}
	oc.cancel()
	oc.wg.Wait()
}

func (oc *orchClient) watchForValsetChanges() error {
	oc.wg.Add(1)
	defer oc.wg.Done()
	results, err := oc.tendermintRPC.Subscribe(oc.ctx, "valset-changes", "tm.event='Tx' AND message.module='qgb'")
	if err != nil {
		return err
	}
	for ev := range results {
		attributes := ev.Events[types.EventTypeValsetRequest]
		for _, attr := range attributes {
			if attr != types.AttributeKeyNonce {
				continue
			}

			queryClient := types.NewQueryClient(oc.qgbRPC)

			lastValsetResp, err := queryClient.LastValsetRequests(oc.ctx, &types.QueryLastValsetRequestsRequest{})
			if err != nil {
				return err
			}

			// todo: double check that the first validator set is found
			if len(lastValsetResp.Valsets) < 1 {
				return errors.New("no validator sets found")
			}

			valset := lastValsetResp.Valsets[0]

			valsetHash := EncodeValsetConfirm(oc.bridgeID, &valset)
			signature, err := oc.personalSignerFn(oc.orchestratorAddr, valsetHash.Bytes())
			if err != nil {
				return err
			}

			// create and send the valset hash
			msg := &types.MsgValsetConfirm{
				Orchestrator: oc.signer.GetSignerInfo().GetAddress().String(),
				EthAddress:   oc.orchestratorAddr.Hex(),
				Nonce:        valset.Nonce,
				Signature:    ethcmn.Bytes2Hex(signature),
			}

			err = oc.broadcastTx(msg)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (oc *orchClient) watchForDataCommitments() error {
	oc.wg.Add(1)
	defer oc.wg.Done()

	queryClient := types.NewQueryClient(oc.qgbRPC)

	resp, err := queryClient.Params(oc.ctx, &types.QueryParamsRequest{})
	if err != nil {
		return err
	}

	params := resp.Params

	results, err := oc.tendermintRPC.Subscribe(oc.ctx, "height", coretypes.EventQueryNewBlockHeader.String())
	if err != nil {
		return err
	}
	for msg := range results {
		eventDataHeader := msg.Data.(coretypes.EventDataNewBlockHeader)
		height := eventDataHeader.Header.Height
		// todo: refactor to ensure that no ranges of blocks are missed if the
		// parameters are changed
		if height%int64(params.DataCommitmentWindow) != 0 {
			continue
		}

		// TODO: calculate start height some other way that can handle changes
		// in the data window param
		startHeight := height - int64(params.DataCommitmentWindow)
		endHeight := height

		// create and send the data commitment
		dcResp, err := oc.tendermintRPC.DataCommitment(
			oc.ctx,
			fmt.Sprintf("block.height >= %d AND block.height <= %d",
				startHeight,
				endHeight,
			),
		)
		if err != nil {
			return err
		}

		// TODO: get nonce using rpc
		nonce := uint64(0)

		dataRootHash := EncodeDataCommitmentConfirm(oc.bridgeID, nonce, dcResp.DataCommitment)

		dcSig, err := oc.personalSignerFn(oc.orchestratorAddr, dataRootHash.Bytes())
		if err != nil {
			return err
		}

		msg := &types.MsgDataCommitmentConfirm{
			EthAddress:       oc.orchestratorAddr.String(),
			Commitment:       dcResp.DataCommitment.String(),
			BeginBlock:       startHeight,
			EndBlock:         endHeight,
			ValidatorAddress: oc.signer.GetSignerInfo().GetAddress().String(),
			Signature:        ethcmn.Bytes2Hex(dcSig),
		}

		err = oc.broadcastTx(msg)
		if err != nil {
			return err
		}

	}
	return nil
}

// TODO: have a way to retry assuming something goes wrong
func (oc *orchClient) broadcastTx(msg sdk.Msg) error {
	err := oc.signer.QueryAccountNumber(oc.ctx, oc.qgbRPC)
	if err != nil {
		return err
	}

	// TODO: update this api via https://github.com/celestiaorg/celestia-app/pull/187/commits/37f96d9af30011736a3e6048bbb35bad6f5b795c
	tx, err := oc.signer.BuildSignedTx(oc.signer.NewTxBuilder(), msg)
	if err != nil {
		return err
	}

	rawTx, err := oc.signer.EncodeTx(tx)
	if err != nil {
		return err
	}

	resp, err := paytypes.BroadcastTx(oc.ctx, oc.qgbRPC, 1, rawTx)
	if err != nil {
		return err
	}

	if resp.TxResponse.Code != 0 {
		return fmt.Errorf("failure to broadcast tx: %s", resp.TxResponse.Data)
	}

	return nil
}

func initEthSigners(
	ethChainID uint64,
	ethPrivKey string,
) (
	ethcmn.Address,
	bind.SignerFn,
	keystore.PersonalSignFn,
	error,
) {
	ethPk, err := ethcrypto.HexToECDSA(ethPrivKey)
	if err != nil {
		return ethcmn.Address{}, nil, nil, fmt.Errorf("failed to hex-decode Ethereum ECDSA Private Key: %w", err)
	}

	addr := ethcrypto.PubkeyToAddress(ethPk.PublicKey)

	txOpts, err := bind.NewKeyedTransactorWithChainID(ethPk, new(big.Int).SetUint64(ethChainID))
	if err != nil {
		return ethcmn.Address{}, nil, nil, fmt.Errorf("failed to init NewKeyedTransactorWithChainID: %w", err)
	}

	personalSignFn, err := keystore.PrivateKeyPersonalSignFn(ethPk)
	if err != nil {
		return ethcmn.Address{}, nil, nil, fmt.Errorf("failed to init PrivateKeyPersonalSignFn: %w", err)
	}

	return addr, txOpts.Signer, personalSignFn, nil
}

func ethPassFromStdin() (string, error) {
	fmt.Fprintln(os.Stderr, "Passphrase for Ethereum account: ")
	bytePassword, err := term.ReadPassword(syscall.Stdin)
	if err != nil {
		return "", fmt.Errorf("failed to read password from STDIN: %w", err)
	}

	password := string(bytePassword)
	return strings.TrimSpace(password), nil
}
