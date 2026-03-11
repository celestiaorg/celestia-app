package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

const SetupFibreSessionName = "setup-fibre"

func setupFibreCmd() *cobra.Command {
	var (
		rootDir      string
		SSHKeyPath   string
		escrowAmount string
		fibrePort    int
		fees         string
		workers      int
	)

	cmd := &cobra.Command{
		Use:   "setup-fibre",
		Short: "Register fibre host addresses and fund escrow accounts on remote validators",
		Long:  "SSHes into each validator and runs two transactions: register the fibre host address and fund the escrow account.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			resolvedSSHKeyPath := resolveValue(SSHKeyPath, EnvVarSSHKeyPath, strings.ReplaceAll(cfg.SSHPubKeyPath, ".pub", ""))

			sem := make(chan struct{}, workers)
			var (
				wg   sync.WaitGroup
				mu   sync.Mutex
				errs []error
			)

			for _, val := range cfg.Validators {
				script := fmt.Sprintf(
					"celestia-appd tx valaddr set-host dns:///%s:%d "+
						"--from validator --keyring-backend=test --home .celestia-app "+
						"--chain-id %s --fees %s --yes;"+
						"sleep 20;"+
						"celestia-appd tx fibre deposit-to-escrow %s "+
						"--from validator --keyring-backend=test --home .celestia-app "+
						"--chain-id %s --fees %s --yes",
					val.PublicIP, fibrePort,
					cfg.ChainID, fees,
					escrowAmount,
					cfg.ChainID, fees,
				)

				sem <- struct{}{}
				wg.Add(1)
				go func(inst Instance, s string) {
					defer wg.Done()
					defer func() { <-sem }()

					fmt.Printf("Running setup-fibre on %s (%s)\n", inst.Name, inst.PublicIP)
					if err := runScriptInTMux([]Instance{inst}, resolvedSSHKeyPath, s, SetupFibreSessionName, time.Minute*5); err != nil {
						mu.Lock()
						errs = append(errs, fmt.Errorf("%s: %w", inst.Name, err))
						mu.Unlock()
					}
				}(val, script)
			}

			wg.Wait()

			if len(errs) > 0 {
				return errors.Join(errs...)
			}
			fmt.Println("Waiting for fibre setup to complete...")
			time.Sleep(40 * time.Second)
			fmt.Println("Done!")
			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to the user's SSH key")
	cmd.Flags().StringVar(&escrowAmount, "escrow-amount", "200000000000000utia", "amount to deposit into escrow")
	cmd.Flags().IntVar(&fibrePort, "fibre-port", 9091, "fibre gRPC port on validators")
	cmd.Flags().StringVar(&fees, "fees", "5000utia", "transaction fees")
	cmd.Flags().IntVarP(&workers, "workers", "w", 10, "number of validators to set up in parallel")

	return cmd
}
