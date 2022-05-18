package network

import (
	"context"
	"fmt"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"math/big"
)

func GetLatestDeployedQGBContract(ctx context.Context, evmRPC string) (*wrapper.QuantumGravityBridge, error) {
	client, err := ethclient.Dial(evmRPC)
	if err != nil {
		return nil, err
	}
	height, err := client.BlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	for i := height; i > 0; i-- {
		block, err := client.BlockByNumber(ctx, big.NewInt(int64(i)))
		if err != nil {
			return nil, err
		}

		for _, tx := range block.Transactions() {
			// If the tx.To is not nil, then it's not a contract creation transaction
			if tx.To() != nil {
				continue
			}
			receipt, err := client.TransactionReceipt(ctx, tx.Hash())
			if err != nil {
				return nil, err
			}
			// TODO check if this check is actually checking if it's
			// If the contract address is 0s or empty, then it's not a contract creation transaction
			if receipt.ContractAddress == (ethcommon.Address{}) {
				continue
			}
			// If the bridge is loaded, then it's the latest deployed QGB contracct
			bridge, err := wrapper.NewQuantumGravityBridge(receipt.ContractAddress, client)
			if err != nil {
				continue
			}
			return bridge, nil
		}
	}
	return nil, fmt.Errorf("no qgb contract found")
}
