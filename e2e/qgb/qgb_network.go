package e2e

import (
	"context"
	"fmt"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/tendermint/tendermint/rpc/client/http"
	"github.com/testcontainers/testcontainers-go"
	"math/big"
	"strings"
	"time"
)

type QGBNetwork struct {
	ComposePaths  []string
	Identifier    string
	Instance      *testcontainers.LocalDockerCompose
	EVMRPC        string
	TendermintRPC string
	CelestiaGRPC  string
}

func NewQGBNetwork() (*QGBNetwork, error) {
	id := strings.ToLower(uuid.New().String())
	paths := []string{"./docker-compose.yml"}
	instance := testcontainers.NewLocalDockerCompose(paths, id)

	return &QGBNetwork{
		Identifier:    id,
		ComposePaths:  paths,
		Instance:      instance,
		EVMRPC:        "http://localhost:8545",
		TendermintRPC: "tcp://localhost:26657",
		CelestiaGRPC:  "localhost:9090",
	}, nil
}

// StartAll starts the whole QGB cluster with multiple validators, orchestrators and a relayer
// Make sure to release the ressources after finishing by calling the `StopAll()` method.
func (network QGBNetwork) StartAll() error {
	// the reason for building before executing `up` is to avoid rebuilding all the images
	// if some container accidentally changed some files when running.
	// This to speed up a bit the execution.
	err := network.Instance.
		WithCommand([]string{"build", "--quiet"}).
		Invoke().Error
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand([]string{"up", "--no-build", "-d"}).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

// StopAll stops the network and leaves the containers created. This allows to resume
// execution from the point where they stopped.
func (network QGBNetwork) StopAll() error {
	err := network.Instance.
		WithCommand([]string{"stop"}).
		Invoke()
	if err.Error != nil {
		return err.Error
	}
	return nil
}

// DeleteAll deletes the containers, network and everything related to the cluster.
func (network QGBNetwork) DeleteAll() error {
	err := network.Instance.
		WithCommand([]string{"down"}).
		Invoke()
	if err.Error != nil {
		return err.Error
	}
	return nil
}

// Start starts a service from the `Service` enum. Make sure to call `Stop`, in the
// end, to release the resources.
func (network QGBNetwork) Start(service Service) error {
	serviceName, err := service.toString()
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand([]string{"build", "--quiet", serviceName}).
		Invoke().Error
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand([]string{"up", "--no-build", "-d", serviceName}).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

// DeployQGBContract uses the Deployer service to deploy a new QGB contract
// based on the existing running network. If no Celestia-app or ganache are
// started, it creates them automatically.
func (network QGBNetwork) DeployQGBContract() error {
	err := network.Instance.
		WithCommand([]string{"build", "--quiet", DEPLOYER}).
		Invoke().Error
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand([]string{"run", "-e", "DEPLOY_NEW_CONTRACT=true", DEPLOYER}).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

// StartMultiple start multiple services. Make sure to call `Stop`, in the
// end, to release the resources.
func (network QGBNetwork) StartMultiple(services ...Service) error {
	if len(services) == 0 {
		return fmt.Errorf("empty list of services provided")
	}
	serviceNames := make([]string, 0)
	for _, s := range services {
		name, err := s.toString()
		if err != nil {
			return err
		}
		serviceNames = append(serviceNames, name)
	}
	err := network.Instance.
		WithCommand(append([]string{"build", "--quiet"}, serviceNames...)).
		Invoke().Error
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand(append([]string{"up", "--no-build", "-d"}, serviceNames...)).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

func (network QGBNetwork) Stop(service Service) error {
	serviceName, err := service.toString()
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand([]string{"stop", serviceName}).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

// StopMultiple start multiple services. Make sure to call `Stop` or `StopMultiple`, in the
// end, to release the resources.
func (network QGBNetwork) StopMultiple(services ...Service) error {
	if len(services) == 0 {
		return fmt.Errorf("empty list of services provided")
	}
	serviceNames := make([]string, 0)
	for _, s := range services {
		name, err := s.toString()
		if err != nil {
			return err
		}
		serviceNames = append(serviceNames, name)
	}
	err := network.Instance.
		WithCommand(append([]string{"up", "-d"}, serviceNames...)).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

// TODO investigate the change on the Dockerfile from entrypoint to command
func (network QGBNetwork) ExecCommand(service Service, command []string) error {
	serviceName, err := service.toString()
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand(append([]string{"exec", serviceName}, command...)).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

// StartMinimal starts a network containing: 1 validator, 1 orchestrator, 1 relayer
// and a ganache instance
func (network QGBNetwork) StartMinimal() error {
	err := network.Instance.
		WithCommand([]string{"build", "--quiet", "core0", "core0-orch", "relayer", "ganache"}).
		Invoke().Error
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand([]string{"up", "--no-build", "-d", "core0", "core0-orch", "relayer", "ganache"}).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

// StartBase starts the very minimal component to have a network.
// It consists of starting `core0` as it is the genesis validator, and the docker network
// will be created along with it, allowing more containers to join it.
func (network QGBNetwork) StartBase() error {
	err := network.Instance.
		WithCommand([]string{"build", "--quiet", "core0"}).
		Invoke().Error
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand([]string{"up", "-d", "--no-build", "core0"}).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

func (network QGBNetwork) WaitForNodeToStart(rpcAddr string) error {
	timeoutChan := time.After(5 * time.Minute)
	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("node %s not initialized in time", rpcAddr)
		default:
			trpc, err := http.New(rpcAddr, "/websocket")
			if err != nil || trpc.Start() != nil {
				fmt.Println("waiting for node to start...")
				time.Sleep(5 * time.Second)
				continue
			}
			return nil
		}
	}
}

func (network QGBNetwork) WaitForBlock(ctx context.Context, height int64) error {
	return network.WaitForBlockWithCustomTimeout(ctx, height, 5*time.Minute)
}

func (network QGBNetwork) WaitForBlockWithCustomTimeout(
	ctx context.Context,
	height int64,
	timeout time.Duration,
) error {
	err := network.WaitForNodeToStart(network.TendermintRPC)
	if err != nil {
		return err
	}
	trpc, err := http.New(network.TendermintRPC, "/websocket")
	if err != nil {
		return err
	}
	err = trpc.Start()
	if err != nil {
		return err
	}
	timeoutChan := time.After(timeout)
	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("chain didn't reach height in time")
		default:
			status, err := trpc.Status(ctx)
			if err != nil || status.SyncInfo.LatestBlockHeight < height {
				fmt.Printf("current height: %d\n", status.SyncInfo.LatestBlockHeight)
				time.Sleep(5 * time.Second)
				continue
			}
			return nil
		}
	}
}

// WaitForOrchestratorToStart waits for the orchestrator having the celes address `accountAddress`
// to sign the first data commitment (could be upgraded to get any signature, either valset or data commitment,
// and for any nonce, but would require adding a new method to the querier. Don't think it is worth it now as
// the number of valsets that will be signed is trivial and reaching 0 would be in no time).
func (network QGBNetwork) WaitForOrchestratorToStart(ctx context.Context, accountAddress string) error {
	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil)
	if err != nil {
		return err
	}
	defer querier.Stop()
	timeoutChan := time.After(5 * time.Minute)
	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("orchestrator didn't start correctly")
		default:
			confirm, err := querier.QueryDataCommitmentConfirm(ctx, types.DataCommitmentWindow, 0, accountAddress)
			if err == nil && confirm != nil {
				return nil
			}
			fmt.Println("waiting for orchestrator to start ...")
			time.Sleep(5 * time.Second)
		}
	}
}

func (network QGBNetwork) GetLatestDeployedQGBContract(ctx context.Context) (*wrapper.QuantumGravityBridge, error) {
	return network.GetLatestDeployedQGBContractWithCustomTimeout(ctx, 5*time.Minute)
}

func (network QGBNetwork) GetLatestDeployedQGBContractWithCustomTimeout(
	ctx context.Context,
	timeout time.Duration,
) (*wrapper.QuantumGravityBridge, error) {
	client, err := ethclient.Dial(network.EVMRPC)
	if err != nil {
		return nil, err
	}
	height := 0
	timeoutChan := time.After(timeout)
	for {
		select {
		case <-timeoutChan:
			return nil, fmt.Errorf("timeout. couldn't find deployed qgb contract")
		default:
			block, err := client.BlockByNumber(ctx, big.NewInt(int64(height)))
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}
			height++
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
	}
}

func (network QGBNetwork) WaitForRelayerToStart(ctx context.Context, bridge *wrapper.QuantumGravityBridge) error {
	timeoutChan := time.After(2 * time.Minute)
	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("relayer didn't start correctly")
		default:
			nonce, err := bridge.StateLastDataRootTupleRootNonce(&bind.CallOpts{Context: ctx})
			if err == nil && nonce != nil && nonce.Int64() >= 1 {
				return nil
			}
			fmt.Println("waiting for relayer to start ...")
			time.Sleep(5 * time.Second)
		}
	}
}

func (network QGBNetwork) PrintLogs() {
	_ = network.Instance.
		WithCommand([]string{"logs"}).
		Invoke()
}
