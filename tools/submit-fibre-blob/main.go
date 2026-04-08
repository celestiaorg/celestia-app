package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		chainID        = flag.String("chain-id", "", "Chain ID of the network (unused, accepted for compatibility)")
		keyName        = flag.String("key-name", "validator", "Key name in keyring")
		keyringBackend = flag.String("keyring-backend", "test", "Keyring backend")
		home           = flag.String("home", "", "Home directory (default: $HOME/.celestia-app)")
		grpcAddr       = flag.String("grpc-addr", "localhost:9090", "gRPC address of the node")
		timeout        = flag.Duration("timeout", 30*time.Second, "Timeout for operations")
	)
	flag.Parse()
	_ = chainID // accepted but unused

	// Set default home directory
	if *home == "" {
		*home = os.Getenv("HOME") + "/.celestia-app"
	}

	// Generate random blob data (1024 bytes)
	blobBytes := make([]byte, 1024)
	if _, err := rand.Read(blobBytes); err != nil {
		return fmt.Errorf("failed to generate random blob: %w", err)
	}

	// Generate random namespace
	nsID := make([]byte, share.NamespaceVersionZeroIDSize)
	if _, err := rand.Read(nsID); err != nil {
		return fmt.Errorf("failed to generate random namespace: %w", err)
	}
	id := make([]byte, 0, share.NamespaceIDSize)
	id = append(id, share.NamespaceVersionZeroPrefix...)
	id = append(id, nsID...)
	ns, err := share.NewNamespace(share.NamespaceVersionZero, id)
	if err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}
	fmt.Printf("Generated random blob (size: %d bytes)\n", len(blobBytes))
	fmt.Printf("Generated random namespace: %s\n", hex.EncodeToString(nsID))

	// Create encoding config
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// Create keyring
	// Note: Currently only supports "test" backend. Other backends can be added if needed.
	if *keyringBackend != "test" {
		return fmt.Errorf("unsupported keyring backend: %s (only 'test' is supported)", *keyringBackend)
	}
	kr, err := keyring.New(app.Name, keyring.BackendTest, *home, nil, encCfg.Codec)
	if err != nil {
		return fmt.Errorf("failed to initialize keyring: %w", err)
	}

	// Create gRPC connection
	grpcConn, err := grpc.NewClient(
		*grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(math.MaxInt32),
			grpc.MaxCallRecvMsgSize(math.MaxInt32),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer grpcConn.Close()

	// Create Fibre client config for single node testnet
	params := fibre.DefaultProtocolParams
	params.MaxValidatorCount = 1 // Single node testnet
	clientCfg := fibre.NewClientConfigFromParams(params)
	clientCfg.StateAddress = *grpcAddr
	clientCfg.DefaultKeyName = *keyName

	// Create context with timeout for network operations
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// Create TxClient
	var txClient *user.TxClient
	if *keyName != "" {
		txClient, err = user.SetupTxClient(ctx, kr, grpcConn, encCfg, user.WithDefaultAccount(*keyName))
	} else {
		txClient, err = user.SetupTxClient(ctx, kr, grpcConn, encCfg)
	}
	if err != nil {
		return fmt.Errorf("failed to set up tx client: %w", err)
	}

	// Create Fibre client
	fibreClient, err := fibre.NewClient(kr, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to create Fibre client: %w", err)
	}
	defer func() {
		if err := fibreClient.Stop(ctx); err != nil {
			log.Printf("stopping fibre client: %v", err)
		}
	}()

	if err := fibreClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start Fibre client: %w", err)
	}

	// Submit blob using Put (which handles upload + PayForFibre transaction)
	fmt.Printf("Submitting Fibre blob (size: %d bytes, namespace: %s)...\n", len(blobBytes), ns.String())
	result, err := fibre.Put(ctx, fibreClient, txClient, ns, blobBytes)
	if err != nil {
		return fmt.Errorf("failed to submit Fibre blob: %w", err)
	}

	fmt.Printf("Successfully submitted Fibre blob!\n")
	fmt.Printf("Transaction hash: %s\n", result.TxHash)
	fmt.Printf("Height: %d\n", result.Height)
	return nil
}
