package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/e2e"
	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
)

const seed = 91284756

func main() {
	celestiaImage := "14ad038"
	if len(os.Args) == 2 {
		celestiaImage = os.Args[1]
	}
	if err := Run(celestiaImage); err != nil {
		log.Fatal(err)
	}
}

func Run(celestiaImage string) error {
	testnet, err := e2e.New("two-node", seed, e2e.GetGrafanaInfoFromEnvVar())
	if err != nil {
		return err
	}
	defer testnet.Cleanup()

	if err := testnet.CreateGenesisNodes(2, celestiaImage, 1e8, 0); err != nil {
		return err
	}

	kr := make([]keyring.Keyring, 2)
	kr[0], err = testnet.CreateAccount("alice", 1e12)
	if err != nil {
		return err
	}
	kr[1], err = testnet.CreateAccount("bob", 1e12)
	if err != nil {
		return err
	}

	if err := testnet.Setup(); err != nil {
		return fmt.Errorf("setup testnet: %w", err)
	}

	if err := testnet.Start(); err != nil {
		return fmt.Errorf("start testnet: %w", err)
	}

	sequences := txsim.NewBlobSequence(txsim.NewRange(1000, 16000), txsim.NewRange(1, 3)).Clone(10)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(10)...)

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	opts := txsim.DefaultOptions().WithSeed(seed)
	// start a tx sim for each node
	errCh := make(chan error, 2)
	for i, endpoint := range testnet.GRPCEndpoints() {
		go func(i int, endpoint string) {
			errCh <- txsim.Run(ctx, endpoint, kr[i], encCfg, opts, sequences...)
		}(i, endpoint)
	}
	for i := 0; i < cap(errCh); i++ {
		if err := <-errCh; !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("tx sim error: %w", err)
		}
	}
	return nil
}
