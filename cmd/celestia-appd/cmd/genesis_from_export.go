package cmd

import (
	"fmt"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/hardfork/genesis"
	"github.com/spf13/cobra"
)

// genesisFromExportCmd builds a candidate genesis from an export file, offline and deterministically.
func genesisFromExportCmd(capp *app.App) *cobra.Command {
	var (
		opts                                         genesis.Options
		blockTime, genesisTime, mintExcess, out, rep string
	)
	cmd := &cobra.Command{
		Use:   "from-export <export.json>",
		Short: "Build a candidate genesis from a celestia-appd export",
		Long: `Build a candidate genesis from a celestia-appd export. The export is checked
against --export-sha256, the candidate gets the given chain-id, initial height
and genesis time, and it is written only after the static, record-level and
conservation checks pass. The output and report must not exist.`,
		Args: cobra.ExactArgs(1),
		// Skip the root pre-run: it reads the home config and env, and the output depends only on the flags.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if opts.ExportBlockTime, err = parseTime("export-block-time", blockTime); err != nil {
				return err
			}
			if opts.GenesisTime, err = parseTime("genesis-time", genesisTime); err != nil {
				return err
			}
			if mintExcess != "" {
				if opts.AcceptMintExcess, err = math.LegacyNewDecFromStr(mintExcess); err != nil {
					return fmt.Errorf("--accept-mint-excess: %w", err)
				}
			}
			if rep == "" {
				rep = out + ".report.json"
			}
			opts.BlockedAddresses = capp.BlockedAddresses()
			cdc := capp.AppCodec()

			exp, err := genesis.LoadExportFile(args[0], genesis.ExportedModules(capp.ModuleManager), opts)
			if err != nil {
				return err
			}
			if err := genesis.CheckGovNulls(&exp.Genesis); err != nil {
				return err
			}
			cand, err := genesis.NewCandidate(cdc, exp, opts)
			if err != nil {
				return err
			}
			// The record checks decode each module with a recover, so ValidateGenesis gets no input they reject.
			res, err := genesis.CheckState(cdc, cand, opts)
			if err != nil {
				return err
			}
			if err := genesis.Validate(cdc, capp.GetTxConfig(), capp.BasicManager, cand); err != nil {
				return err
			}
			report := genesis.NewReport(exp, cand, opts)
			report.Checks = res
			if err := genesis.Install(cand, report, args[0], out, rep); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "genesis_sha256 %s\nreport %s\n", report.GenesisSHA256, rep)
			return err
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.ExportSHA256, "export-sha256", "", "hex SHA-256 of the export file")
	f.Int64Var(&opts.ExportHeight, "export-height", 0, "last executed height E of the exported chain")
	f.StringVar(&blockTime, "export-block-time", "", "block time T of the export height (RFC3339)")
	f.StringVar(&genesisTime, "genesis-time", "", "genesis time G of the new chain (RFC3339), >= T")
	f.Int64Var(&opts.InitialHeight, "initial-height", 0, "initial height of the new chain: >= E+11 for the same chain-id, >= E+1 otherwise")
	f.StringVar(&out, "output", "", "path of the candidate genesis")
	f.StringVar(&opts.ChainID, "chain-id", "", "chain-id of the new chain (default: the export's)")
	f.StringVar(&rep, "report", "", "path of the report (default: <output>.report.json)")
	f.StringVar(&mintExcess, "accept-mint-excess", "", "acknowledge the mint excess; must equal the computed value exactly")
	for _, name := range []string{"export-sha256", "export-height", "export-block-time", "genesis-time", "initial-height", "output"} {
		if err := cmd.MarkFlagRequired(name); err != nil {
			panic(err)
		}
	}
	return cmd
}

func parseTime(flag, s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--%s: %w", flag, err)
	}
	return t, nil
}
