package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"syscall"

	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/celestiaorg/quantum-gravity-bridge/orchestrator/ethereum/keystore"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcmn "github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
	"github.com/tendermint/tendermint/rpc/client/http"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
	coretypes "github.com/tendermint/tendermint/types"
	"golang.org/x/term"
	"google.golang.org/grpc"
)

type orchClient struct {
	// administrativa
	wg     *sync.WaitGroup
	logger zerolog.Logger

	// RPC
	tendermintRPC *http.HTTP
	qgbRPC        *grpc.ClientConn
	ethRPC        *ethclient.Client
	wrapper       *wrapper.QuantumGravityBridge

	// orchestrator signing
	singerFn           bind.SignerFn
	personalSignerFn   keystore.PersonalSignFn
	transactOpsBuilder transactOpsBuilder
	evmAddress         ethcmn.Address
	bridgeID           ethcmn.Hash

	// celestia related signing
	signer *paytypes.KeyringSigner
}

func newOrchClient(logger zerolog.Logger, appSigner *paytypes.KeyringSigner, cfg config) (*orchClient, error) {
	trpc, err := http.New(cfg.tendermintRPC, "/websocket")
	if err != nil {
		return nil, err
	}

	qgbGRPC, err := grpc.Dial(cfg.qgbRPC, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	ethclient, err := ethclient.Dial(cfg.evmRPC)
	if err != nil {
		return nil, err
	}

	qgbWrapper, err := wrapper.NewQuantumGravityBridge(cfg.contractAddr, ethclient)
	if err != nil {
		return nil, err
	}

	orchAddr, sfn, psfn, err := initEthSigners(cfg.evmChainID, cfg.privateKey)
	if err != nil {
		return nil, err
	}

	transactOpsBuilder := newTransactOptsBuilder(cfg.privateKey)

	return &orchClient{
		tendermintRPC:      trpc,
		singerFn:           sfn,
		personalSignerFn:   psfn,
		transactOpsBuilder: transactOpsBuilder,
		logger:             logger,
		qgbRPC:             qgbGRPC,
		wg:                 &sync.WaitGroup{},
		evmAddress:         orchAddr,
		signer:             appSigner,
		bridgeID:           cfg.bridgeID,
		wrapper:            qgbWrapper,
	}, nil
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
	oc.wg.Wait()
}

func (oc *orchClient) watchForValsetChanges(ctx context.Context) error {
	oc.wg.Add(1)
	defer oc.wg.Done()
	results, err := oc.tendermintRPC.Subscribe(ctx, "valset-changes", "tm.event='Tx' AND message.module='qgb'")
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

			lastValsetResp, err := queryClient.LastValsetRequests(ctx, &types.QueryLastValsetRequestsRequest{})
			if err != nil {
				return err
			}

			// todo: double check that the first validator set is found
			if len(lastValsetResp.Valsets) < 1 {
				return errors.New("no validator sets found")
			}

			valset := lastValsetResp.Valsets[0]
			height := int64(valset.Height)

			// we need the validator set hash for this height.
			blockRes, err := oc.tendermintRPC.Block(ctx, &height)
			if err != nil {
				return err
			}

			rawVSHash := blockRes.Block.Header.ValidatorsHash.Bytes()
			var ethVSHash ethcmn.Hash
			copy(ethVSHash[:], rawVSHash)

			signBytes := EncodeValsetConfirm(oc.bridgeID, &valset, ethVSHash)

			signature, err := oc.personalSignerFn(oc.evmAddress, signBytes.Bytes())
			if err != nil {
				return err
			}

			// create and send the valset hash
			msg := &types.MsgValsetConfirm{
				Orchestrator: oc.signer.GetSignerInfo().GetAddress().String(),
				EthAddress:   oc.evmAddress.Hex(),
				Nonce:        valset.Nonce,
				Signature:    ethcmn.Bytes2Hex(signature),
			}

			err = oc.broadcastTx(ctx, msg)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (oc *orchClient) watchForDataCommitments(ctx context.Context) error {
	oc.wg.Add(1)
	defer oc.wg.Done()

	queryClient := types.NewQueryClient(oc.qgbRPC)

	resp, err := queryClient.Params(ctx, &types.QueryParamsRequest{})
	if err != nil {
		return err
	}

	params := resp.Params

	results, err := oc.tendermintRPC.Subscribe(ctx, "height", coretypes.EventQueryNewBlockHeader.String())
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-results:
			oc.processDataCommitmentEvents(ctx, params.DataCommitmentWindow, ev)
		}
	}
}

func (oc *orchClient) processDataCommitmentEvents(ctx context.Context, window uint64, ev rpctypes.ResultEvent) error {
	eventDataHeader := ev.Data.(coretypes.EventDataNewBlockHeader)
	height := eventDataHeader.Header.Height
	// todo: refactor to ensure that no ranges of blocks are missed if the
	// parameters are changed
	if height%int64(window) != 0 {
		return nil
	}

	// TODO: calculate start height some other way that can handle changes
	// in the data window param
	startHeight := height - int64(window)
	endHeight := height

	// create and send the data commitment
	dcResp, err := oc.tendermintRPC.DataCommitment(
		ctx,
		fmt.Sprintf("block.height >= %d AND block.height <= %d",
			startHeight,
			endHeight,
		),
	)
	if err != nil {
		return err
	}

	nonce, err := oc.wrapper.StateLastMessageTupleRootNonce(&bind.CallOpts{})
	if err != nil {
		return err
	}

	nonce.Add(nonce, big.NewInt(1))

	dataRootHash := EncodeDataCommitmentConfirm(oc.bridgeID, nonce, dcResp.DataCommitment)

	dcSig, err := oc.personalSignerFn(oc.evmAddress, dataRootHash.Bytes())
	if err != nil {
		return err
	}

	msg := &types.MsgDataCommitmentConfirm{
		EthAddress:       oc.evmAddress.String(),
		Commitment:       dcResp.DataCommitment.String(),
		BeginBlock:       startHeight,
		EndBlock:         endHeight,
		ValidatorAddress: oc.signer.GetSignerInfo().GetAddress().String(),
		Signature:        ethcmn.Bytes2Hex(dcSig),
	}

	return oc.broadcastTx(ctx, msg)
}

// TODO: have a way to retry assuming something goes wrong
func (oc *orchClient) broadcastTx(ctx context.Context, msg sdk.Msg) error {
	err := oc.signer.QueryAccountNumber(ctx, oc.qgbRPC)
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

	resp, err := paytypes.BroadcastTx(ctx, oc.qgbRPC, 1, rawTx)
	if err != nil {
		return err
	}

	if resp.TxResponse.Code != 0 {
		return fmt.Errorf("failure to broadcast tx: %s", resp.TxResponse.Data)
	}

	return nil
}

// TODO: make gas price configurable
type transactOpsBuilder func(ctx context.Context, client *ethclient.Client, gasLim uint64) (*bind.TransactOpts, error)

func newTransactOptsBuilder(privKey *ecdsa.PrivateKey) transactOpsBuilder {
	publicKey := privKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		panic(fmt.Errorf("invalid public key; expected: %T, got: %T", &ecdsa.PublicKey{}, publicKey))
	}

	evmAddress := ethcrypto.PubkeyToAddress(*publicKeyECDSA)
	return func(ctx context.Context, client *ethclient.Client, gasLim uint64) (*bind.TransactOpts, error) {
		nonce, err := client.PendingNonceAt(ctx, evmAddress)
		if err != nil {
			return nil, err
		}

		ethChainID, err := client.ChainID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get Ethereum chain ID: %w", err)
		}

		auth, err := bind.NewKeyedTransactorWithChainID(privKey, ethChainID)
		if err != nil {
			return nil, fmt.Errorf("failed to create Ethereum transactor: %w", err)
		}

		bigGasPrice, err := client.SuggestGasPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get Ethereum gas estimate: %w", err)
		}

		auth.Nonce = new(big.Int).SetUint64(nonce)
		auth.Value = big.NewInt(0) // in wei
		auth.GasLimit = gasLim     // in units
		auth.GasPrice = bigGasPrice

		return auth, nil
	}
}

func initEthSigners(
	ethChainID uint64,
	ethPrivKey *ecdsa.PrivateKey,
) (
	ethcmn.Address,
	bind.SignerFn,
	keystore.PersonalSignFn,
	error,
) {

	addr := ethcrypto.PubkeyToAddress(ethPrivKey.PublicKey)

	txOpts, err := bind.NewKeyedTransactorWithChainID(ethPrivKey, new(big.Int).SetUint64(ethChainID))
	if err != nil {
		return ethcmn.Address{}, nil, nil, fmt.Errorf("failed to init NewKeyedTransactorWithChainID: %w", err)
	}

	personalSignFn, err := keystore.PrivateKeyPersonalSignFn(ethPrivKey)
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
