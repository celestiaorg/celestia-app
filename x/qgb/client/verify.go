package client

import (
	"context"
	"encoding/hex"
	"math/big"
	"os"
	"strconv"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/tendermint/tendermint/crypto/merkle"

	"github.com/celestiaorg/celestia-app/pkg/prove"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/spf13/cobra"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/rpc/client/http"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func VerifyCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "verify",
		Short: "Verifies that a transaction hash, a range of shares, or a message referenced by its transaction hash were committed to by the QGB contract",
	}
	command.AddCommand(
		txCmd(),
		sharesCmd(),
		msgCmd(),
	)
	return command
}

func txCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "tx <tx_hash>",
		Args:  cobra.ExactArgs(1),
		Short: "Verifies that a transaction hash, in hex format, has been committed to by the QGB contract",
		RunE: func(cmd *cobra.Command, args []string) error {
			txHash, err := hex.DecodeString(args[0])
			if err != nil {
				return err
			}

			config, err := parseVerifyFlags(cmd)
			if err != nil {
				return err
			}

			logger := tmlog.NewTMLogger(os.Stdout)

			trpc, err := http.New(config.TendermintRPC, "/websocket")
			if err != nil {
				return err
			}
			err = trpc.Start()
			if err != nil {
				return err
			}
			defer func(trpc *http.HTTP) {
				err := trpc.Stop()
				if err != nil {
					logger.Debug("error closing connection", "err", err.Error())
				}
			}(trpc)

			tx, err := trpc.Tx(cmd.Context(), txHash, true)
			if err != nil {
				return err
			}

			logger.Info("verifying that the transaction was committed to by the QGB", "tx_hash", args[0], "height", tx.Height)

			blockRes, err := trpc.Block(cmd.Context(), &tx.Height)
			if err != nil {
				return err
			}

			beginTxShare, endTxShare, err := prove.TxSharePosition(blockRes.Block.Txs, uint64(tx.Index))
			if err != nil {
				return err
			}

			_, err = VerifyShares(cmd.Context(), logger, config, uint64(tx.Height), beginTxShare, endTxShare)
			return err
		},
	}
	return addVerifyFlags(command)
}

func msgCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "msg <tx_hash>",
		Args:  cobra.ExactArgs(1),
		Short: "Verifies that a message, referenced by its transaction hash, in hex format, has been committed to by the QGB contract. Only supports one message for now",
		RunE: func(cmd *cobra.Command, args []string) error {
			txHash, err := hex.DecodeString(args[0])
			if err != nil {
				return err
			}

			config, err := parseVerifyFlags(cmd)
			if err != nil {
				return err
			}

			logger := tmlog.NewTMLogger(os.Stdout)

			trpc, err := http.New(config.TendermintRPC, "/websocket")
			if err != nil {
				return err
			}
			err = trpc.Start()
			if err != nil {
				return err
			}
			defer func(trpc *http.HTTP) {
				err := trpc.Stop()
				if err != nil {
					logger.Debug("error closing connection", "err", err.Error())
				}
			}(trpc)

			tx, err := trpc.Tx(cmd.Context(), txHash, true)
			if err != nil {
				return err
			}

			logger.Info("verifying that the message was committed to by the QGB", "tx_hash", args[0], "height", tx.Height)

			blockRes, err := trpc.Block(cmd.Context(), &tx.Height)
			if err != nil {
				return err
			}

			beginMsgShare, endMsgShare, err := prove.BlobShareRange(blockRes.Block.Txs[tx.Index])
			if err != nil {
				return err
			}

			_, err = VerifyShares(cmd.Context(), logger, config, uint64(tx.Height), beginMsgShare, endMsgShare)
			return err
		},
	}
	return addVerifyFlags(command)
}

func sharesCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "shares <height> <start_share> <end_share>",
		Args:  cobra.ExactArgs(3),
		Short: "Verifies that a shares range has been committed to by the QGB contract",
		RunE: func(cmd *cobra.Command, args []string) error {
			height, err := strconv.ParseUint(args[0], 10, 0)
			if err != nil {
				return err
			}
			startShare, err := strconv.ParseUint(args[1], 10, 0)
			if err != nil {
				return err
			}
			endShare, err := strconv.ParseUint(args[2], 10, 0)
			if err != nil {
				return err
			}

			config, err := parseVerifyFlags(cmd)
			if err != nil {
				return err
			}

			logger := tmlog.NewTMLogger(os.Stdout)

			_, err = VerifyShares(cmd.Context(), logger, config, height, startShare, endShare)
			return err
		},
	}
	return addVerifyFlags(command)
}

func VerifyShares(ctx context.Context, logger tmlog.Logger, config VerifyConfig, height uint64, startShare uint64, endShare uint64) (isCommittedTo bool, err error) {
	trpc, err := http.New(config.TendermintRPC, "/websocket")
	if err != nil {
		return
	}
	err = trpc.Start()
	if err != nil {
		return
	}
	defer func(trpc *http.HTTP) {
		err := trpc.Stop()
		if err != nil {
			logger.Debug("error closing connection", "err", err.Error())
		}
	}(trpc)

	logger.Info(
		"proving shares inclusion to data root",
		"height",
		height,
		"start_share",
		startShare,
		"end_share",
		endShare,
	)

	logger.Debug("getting shares proof from tendermint node")
	sharesProofs, err := trpc.ProveShares(ctx, height, startShare, endShare)
	if err != nil {
		return
	}

	logger.Debug("verifying shares proofs")
	// checks if the shares proof is valid.
	// the shares proof is self verifiable because it contains also the rows roots
	// which the nmt shares proof is verified against.
	if !sharesProofs.VerifyProof() {
		logger.Info("proofs from shares to data root are invalid")
		return
	}

	logger.Info("proofs from shares to data root are valid")

	qgbGRPC, err := grpc.Dial(config.CelesGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return
	}
	defer func(qgbGRPC *grpc.ClientConn) {
		err := qgbGRPC.Close()
		if err != nil {
			logger.Debug("error closing connection", "err", err.Error())
		}
	}(qgbGRPC)

	queryClient := types.NewQueryClient(qgbGRPC)

	blocksRange, err := queryClient.DataCommitmentRangeForHeight(
		ctx,
		&types.QueryDataCommitmentRangeForHeightRequest{Height: height},
	)
	if err != nil {
		return
	}

	logger.Info(
		"proving that the data root was committed to in the QGB contract",
		"contract_address",
		config.ContractAddr,
		"fist_block",
		blocksRange.BeginBlock,
		"last_block",
		blocksRange.EndBlock,
		"nonce",
		blocksRange.Nonce,
	)

	logger.Debug("getting the data root to commitment inclusion proof")
	dcProof, err := trpc.DataRootInclusionProof(ctx, height, blocksRange.BeginBlock, blocksRange.EndBlock)
	if err != nil {
		return
	}

	heightI := int64(height)
	block, err := trpc.Block(ctx, &heightI)
	if err != nil {
		return
	}

	ethClient, err := ethclient.Dial(config.EVMRPC)
	if err != nil {
		return
	}
	defer ethClient.Close()

	qgbWrapper, err := wrapper.NewQuantumGravityBridge(config.ContractAddr, ethClient)
	if err != nil {
		return
	}

	logger.Info("verifying that the data root was committed to in the QGB contract")
	isCommittedTo, err = VerifyDataRootInclusion(
		ctx,
		qgbWrapper,
		blocksRange.Nonce,
		height,
		block.Block.DataHash,
		dcProof.Proof,
	)
	if err != nil {
		return
	}

	if isCommittedTo {
		logger.Info("the QGB contract has committed to the provided shares")
	} else {
		logger.Info("the QGB contract didn't commit to the provided shares")
	}

	return
}

func VerifyDataRootInclusion(
	ctx context.Context,
	qgbWrapper *wrapper.QuantumGravityBridge,
	nonce uint64,
	height uint64,
	dataRoot []byte,
	proof merkle.Proof,
) (bool, error) {
	tuple := wrapper.DataRootTuple{
		Height:   big.NewInt(int64(height)),
		DataRoot: *(*[32]byte)(dataRoot),
	}

	sideNodes := make([][32]byte, len(proof.Aunts))
	for i, aunt := range proof.Aunts {
		sideNodes[i] = *(*[32]byte)(aunt)
	}
	wrappedProof := wrapper.BinaryMerkleProof{
		SideNodes: sideNodes,
		Key:       big.NewInt(proof.Index),
		NumLeaves: big.NewInt(proof.Total),
	}

	valid, err := qgbWrapper.VerifyAttestation(
		&bind.CallOpts{},
		big.NewInt(int64(nonce)),
		tuple,
		wrappedProof,
	)
	if err != nil {
		return false, err
	}
	return valid, nil
}
