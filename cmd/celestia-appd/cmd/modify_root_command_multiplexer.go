//go:build multiplexer

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/multiplexer/abci"
	"github.com/celestiaorg/celestia-app/v4/multiplexer/appd"
	multiplexer "github.com/celestiaorg/celestia-app/v4/multiplexer/cmd"
	"github.com/cosmos/cosmos-sdk/server"

	embedding "github.com/celestiaorg/celestia-app/v4/internal/embedding"
)

// modifyRootCommand enhances the root command with the pass through and multiplexer.
func modifyRootCommand(rootCommand *cobra.Command) {
	version, compressedBinary, err := embedding.CelestiaAppV3()
	if err != nil {
		panic(err)
	}

	appdV3, err := appd.New(version, compressedBinary)
	if err != nil {
		panic(err)
	}

	var (
		embeddedArgsFile string
		embeddedArgs     EmbeddedArgs
	)
	// get the home directory from the flag
	args := os.Args
	for i := 0; i < len(args); i++ {
		if args[i] == fmt.Sprintf("--%s", FlagEmbeddedArgsFile) && i+1 < len(args) {
			embeddedArgsFile = filepath.Clean(args[i+1])
		} else if strings.HasPrefix(args[i], fmt.Sprintf("--%s=", FlagEmbeddedArgsFile)) {
			embeddedArgsFile = filepath.Clean(args[i][7:])
		}
	}

	if len(embeddedArgsFile) > 0 {
		f, err := os.ReadFile(embeddedArgsFile)
		if os.IsNotExist(err) {
			panic(fmt.Sprintf("file %s does not exist: %s", err.Error()))
		} else if err != nil {
			panic(err)
		}

		if err := json.Unmarshal(f, &embeddedArgs); err != nil {
			panic(err)
		}

		// Expand environment variables in embedded args
		embeddedArgs.V3 = expandEnvVars(embeddedArgs.V3)
	}

	versions, err := abci.NewVersions(abci.Version{
		Appd:        appdV3,
		ABCIVersion: amodify_root_command_multiplexerbci.ABCIClientVersion1,
		AppVersion:  3,
		StartArgs:   embeddedArgs.V3,
	})
	if err != nil {
		panic(err)
	}

	rootCommand.AddCommand(
		multiplexer.NewPassthroughCmd(versions),
	)

	// Add the following commands to the rootCommand: start, tendermint, export, version, and rollback and wire multiplexer.
	server.AddCommandsWithStartCmdOptions(
		rootCommand,
		app.NodeHome,
		NewAppServer,
		appExporter,
		server.StartCmdOptions{
			AddFlags:            addStartFlags,
			StartCommandHandler: multiplexer.New(versions),
		},
	)
}

// EmbeddedArgs is a map of arguments that can be passed to the multiplexer.
// Each key corresponds to the binary name, and the value is a slice of strings representing the arguments.
type EmbeddedArgs struct {
	V3 []string `json:"v3"`
	// For new embedded binaries, add them here.
	// VX []string `json:"vX"`
}

// expandEnvVars expands environment variables in the given slice of strings.
// It uses os.ExpandEnv to substitute variables of the form $VAR or ${VAR}.
func expandEnvVars(args []string) []string {
	expanded := make([]string, len(args))
	for i, arg := range args {
		expanded[i] = os.ExpandEnv(arg)
	}
	return expanded
}
