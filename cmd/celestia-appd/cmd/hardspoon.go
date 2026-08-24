package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/app/hardspoon"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/spf13/cobra"
)

const (
	flagGenesisTime   = "genesis-time"
	flagInitialHeight = "initial-height"
	flagAppVersion    = "app-version"
	flagReserves      = "reserves"
	flagKeepPubKeys   = "keep-pubkeys"
	flagNoPrune       = "no-prune"
	flagMaxSizeBytes  = "max-size-bytes"
	flagReport        = "report"
)

// hardspoonCmd builds a new chain's genesis from an exported one.
func hardspoonCmd(capp *app.App) *cobra.Command {
	command := &cobra.Command{
		Use:   "hardspoon [chain-id] [exported-genesis] [output-genesis]",
		Short: "Build a new chain's genesis from an `export --for-zero-height` snapshot",
		Long: `Build a new chain's genesis from an "export --for-zero-height" snapshot.

Delegations, unbonding delegations and accrued rewards are redeemed to the
accounts that own them as liquid tokens. Module-account funds, provably
unreachable balances (interchain accounts, IBC transfer escrow) and orphaned
bridged assets (synthetic Hyperlane warp coins, IBC transfer vouchers), whose
redemption paths do not survive the spoon, are dropped, and the supply is
recomputed from the surviving balances. Modules that governance can tune are
carried across so the new chain is a parameter-level replica of the old one;
everything else starts from this binary's default genesis.

The exported genesis must come from "export --for-zero-height". A plain export
leaves each delegation's recorded stake stale for any validator that was later
slashed, which would over-credit delegators, so it is rejected.

The output is written compact, because the genesis app_state is passed to
InitChain verbatim and has to fit under the ABCI receive limit. Note that
"genesis collect-gentxs" and "debug convert-genesis" both re-indent the file, so
re-compact it before hashing and publishing, then run "hardspoon verify" on the
result.`,
		Args: cobra.ExactArgs(3),
		RunE: func(command *cobra.Command, args []string) error {
			opts, err := hardspoonOptions(command)
			if err != nil {
				return err
			}
			opts.ChainID = args[0]
			// Every module validates its own genesis, so a record that is legal
			// on a live chain but illegal in a genesis file fails here instead of
			// inside a later `genesis` subcommand.
			opts.ValidateGenesis = func(appState map[string]json.RawMessage) error {
				return capp.BasicManager.ValidateGenesis(capp.AppCodec(), capp.GetTxConfig(), appState)
			}

			raw, err := os.ReadFile(args[1])
			if err != nil {
				return fmt.Errorf("reading exported genesis: %w", err)
			}
			fork, err := hardspoon.LoadFork(raw)
			if err != nil {
				return err
			}

			result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), fork, opts)
			if err != nil {
				return err
			}

			if err := os.WriteFile(args[2], result.Bytes, 0o644); err != nil {
				return fmt.Errorf("writing genesis: %w", err)
			}

			reportPath, _ := command.Flags().GetString(flagReport)
			if reportPath != "" {
				encoded, err := json.MarshalIndent(result.Report, "", "  ")
				if err != nil {
					return fmt.Errorf("encoding report: %w", err)
				}
				if err := os.WriteFile(reportPath, encoded, 0o644); err != nil {
					return fmt.Errorf("writing report: %w", err)
				}
			}

			command.Println(result.Report.String())
			command.Printf("wrote %s (%d bytes)\n", args[2], len(result.Bytes))
			return nil
		},
	}

	command.Flags().String(flagGenesisTime, "", "genesis time of the new chain, RFC3339 (required)")
	command.Flags().Int64(flagInitialHeight, 1, "initial height of the new chain")
	command.Flags().Uint64(flagAppVersion, appconsts.Version, "app version written to consensus params")
	command.Flags().String(flagReserves, "", `JSON file of new funded accounts: [{"address":"celestia1...","amount":"100000000000000utia"}]`)
	command.Flags().Bool(flagKeepPubKeys, false, "keep account public keys instead of dropping them (costs several MiB; they are re-learned from each account's first signed tx)")
	command.Flags().Bool(flagNoPrune, false, "keep accounts that hold no funds")
	command.Flags().Int(flagMaxSizeBytes, hardspoon.DefaultMaxSizeBytes, "fail if the serialized genesis exceeds this many bytes")
	command.Flags().String(flagReport, "", "also write the reconciliation report to this file as JSON")

	command.AddCommand(hardspoonVerifyCmd(capp))
	return command
}

// hardspoonVerifyCmd re-checks a genesis that hardspoon produced.
func hardspoonVerifyCmd(capp *app.App) *cobra.Command {
	command := &cobra.Command{
		Use:   "verify [genesis]",
		Short: "Re-check the invariants hardspoon establishes",
		Long: `Re-check the invariants hardspoon establishes.

Run this on the final published genesis. Between hardspoon and publication the
file passes through "genesis collect-gentxs" and "debug convert-genesis", both of
which rewrite it, so this confirms the app version, bank and auth parity, the
recomputed supply (with no orphaned denoms), the emptied staking, distribution,
slashing, gov, hyperlane and transfer state, and the minfee parameters all
survived.`,
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("reading genesis: %w", err)
			}

			opts, err := hardspoonOptions(command)
			if err != nil {
				return err
			}

			genesis, err := hardspoon.VerifyFile(capp.AppCodec(), raw, opts)
			if err != nil {
				return err
			}

			command.Printf(
				"%s: chain-id %s, app version %d, initial height %d, %d bytes\n",
				args[0], genesis.ChainID, genesis.ConsensusParams.Version.App,
				genesis.InitialHeight, len(raw),
			)
			return nil
		},
	}

	command.Flags().Uint64(flagAppVersion, appconsts.Version, "app version the genesis must declare")
	command.Flags().Int(flagMaxSizeBytes, hardspoon.DefaultMaxSizeBytes, "fail if the genesis exceeds this many bytes")
	return command
}

func hardspoonOptions(command *cobra.Command) (hardspoon.Options, error) {
	var opts hardspoon.Options

	// verify does not take these, so a missing flag is not an error.
	if command.Flags().Lookup(flagGenesisTime) != nil {
		raw, err := command.Flags().GetString(flagGenesisTime)
		if err != nil {
			return opts, err
		}
		if raw == "" {
			return opts, fmt.Errorf("--%s is required", flagGenesisTime)
		}
		genesisTime, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return opts, fmt.Errorf("parsing --%s: %w", flagGenesisTime, err)
		}
		opts.GenesisTime = genesisTime.UTC()
	}
	if command.Flags().Lookup(flagInitialHeight) != nil {
		height, err := command.Flags().GetInt64(flagInitialHeight)
		if err != nil {
			return opts, err
		}
		opts.InitialHeight = height
	}
	if command.Flags().Lookup(flagKeepPubKeys) != nil {
		keep, err := command.Flags().GetBool(flagKeepPubKeys)
		if err != nil {
			return opts, err
		}
		opts.KeepPubKeys = keep
	}
	if command.Flags().Lookup(flagNoPrune) != nil {
		noPrune, err := command.Flags().GetBool(flagNoPrune)
		if err != nil {
			return opts, err
		}
		opts.NoPrune = noPrune
	}

	appVersion, err := command.Flags().GetUint64(flagAppVersion)
	if err != nil {
		return opts, err
	}
	opts.AppVersion = appVersion

	maxSize, err := command.Flags().GetInt(flagMaxSizeBytes)
	if err != nil {
		return opts, err
	}
	opts.MaxSizeBytes = maxSize

	if command.Flags().Lookup(flagReserves) != nil {
		path, err := command.Flags().GetString(flagReserves)
		if err != nil {
			return opts, err
		}
		if path != "" {
			opts.Reserves, err = readReserves(path)
			if err != nil {
				return opts, err
			}
		}
	}

	return opts, nil
}

func readReserves(path string) ([]hardspoon.Reserve, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading reserves: %w", err)
	}
	var reserves []hardspoon.Reserve
	if err := json.Unmarshal(raw, &reserves); err != nil {
		return nil, fmt.Errorf("parsing reserves: %w", err)
	}
	if len(reserves) == 0 {
		return nil, fmt.Errorf("reserves file %s is empty", path)
	}
	return reserves, nil
}
