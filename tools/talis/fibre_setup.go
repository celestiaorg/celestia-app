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
		rootDir              string
		SSHKeyPath           string
		escrowAmount         string
		fibrePort            int
		fees                 string
		workers              int
		fibreAccounts        int
		encoderFibreAccounts int
	)

	cmd := &cobra.Command{
		Use:   "setup-fibre",
		Short: "Register fibre host addresses and fund escrow accounts on remote validators",
		Long:  "SSHes into each validator and runs transactions: register the fibre host address and fund escrow accounts for the validator and all fibre worker accounts.",
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
				// Build script: register host + deposit escrow for validator + all fibre accounts
				var sb strings.Builder

				// 1. Register fibre host address
				sb.WriteString(fmt.Sprintf(
					"celestia-appd tx valaddr set-host dns:///%s:%d "+
						"--from validator --keyring-backend=test --home .celestia-app "+
						"--chain-id %s --fees %s --yes\n",
					val.PublicIP, fibrePort,
					cfg.ChainID, fees,
				))
				sb.WriteString("sleep 10\n")

				// 2. Deposit escrow for each fibre worker account
				for i := 0; i < fibreAccounts; i++ {
					keyName := fmt.Sprintf("fibre-%d", i)
					sb.WriteString(fmt.Sprintf(
						"celestia-appd tx fibre deposit-to-escrow %s "+
							"--from %s --keyring-backend=test --home .celestia-app "+
							"--chain-id %s --fees %s --yes\n",
						escrowAmount,
						keyName,
						cfg.ChainID, fees,
					))
				}

				script := sb.String()

				sem <- struct{}{}
				wg.Add(1)
				go func(inst Instance, s string) {
					defer wg.Done()
					defer func() { <-sem }()

					fmt.Printf("Running setup-fibre on %s (%s) — registering host + %d escrow deposits\n", inst.Name, inst.PublicIP, fibreAccounts)
					if err := runScriptInTMux([]Instance{inst}, resolvedSSHKeyPath, s, SetupFibreSessionName, time.Minute*30); err != nil {
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

			fmt.Printf("Waiting for fibre setup to complete (%d accounts per validator)...\n", fibreAccounts)
			if err := waitForTmuxSessions(cfg.Validators, resolvedSSHKeyPath, SetupFibreSessionName, 10*time.Minute); err != nil {
				return fmt.Errorf("waiting for setup-fibre sessions: %w", err)
			}
			fmt.Println("Validator setup done!")

			// Deposit escrow for encoder accounts.
			// Each encoder runs deposit-to-escrow from its own machine using its
			// own keyring, broadcasting via the first validator's RPC endpoint.
			if len(cfg.Encoders) > 0 && len(cfg.Validators) > 0 {
				rpcNode := fmt.Sprintf("tcp://%s:26657", cfg.Validators[0].PublicIP)
				fmt.Printf("Setting up escrow for %d encoder(s) via %s...\n", len(cfg.Encoders), rpcNode)

				for _, enc := range cfg.Encoders {
					encIndex := extractIndexFromName(enc.Name)
					keyPrefix := fmt.Sprintf("enc%d", encIndex)
					nAccounts := encoderFibreAccounts

					var sb strings.Builder
					for i := 0; i < nAccounts; i++ {
						keyName := fmt.Sprintf("%s-%d", keyPrefix, i)
						sb.WriteString(fmt.Sprintf(
							"celestia-appd tx fibre deposit-to-escrow %s "+
								"--from %s --keyring-backend=test --home .celestia-app "+
								"--chain-id %s --fees %s --node %s --yes\n",
							escrowAmount,
							keyName,
							cfg.ChainID, fees, rpcNode,
						))
					}

					script := sb.String()
					fmt.Printf("Running escrow deposits on encoder %s (%s) — %d accounts\n", enc.Name, enc.PublicIP, nAccounts)
					if err := runScriptInTMux([]Instance{enc}, resolvedSSHKeyPath, script, SetupFibreSessionName, 30*time.Minute); err != nil {
						return fmt.Errorf("encoder %s escrow setup: %w", enc.Name, err)
					}
				}

				fmt.Printf("Waiting for encoder escrow deposits to complete...\n")
				if err := waitForTmuxSessions(cfg.Encoders, resolvedSSHKeyPath, SetupFibreSessionName, 15*time.Minute); err != nil {
					return fmt.Errorf("waiting for encoder setup-fibre sessions: %w", err)
				}
				fmt.Println("Encoder escrow setup done!")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "path to the user's SSH key")
	cmd.Flags().StringVar(&escrowAmount, "escrow-amount", "200000000000000utia", "amount to deposit into escrow")
	cmd.Flags().IntVar(&fibrePort, "fibre-port", 7980, "fibre gRPC port on validators")
	cmd.Flags().StringVar(&fees, "fees", "5000utia", "transaction fees")
	cmd.Flags().IntVarP(&workers, "workers", "w", 10, "number of validators to set up in parallel")
	cmd.Flags().IntVar(&fibreAccounts, "fibre-accounts", 100, "number of fibre worker accounts to deposit escrow for")
	cmd.Flags().IntVar(&encoderFibreAccounts, "encoder-fibre-accounts", 100, "number of fibre worker accounts per encoder instance")

	return cmd
}
