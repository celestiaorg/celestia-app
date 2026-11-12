// Package genesis builds a candidate genesis from a celestia-appd export.
package genesis

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/types/module"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

// Module is one app_state entry; Raw is shared with copies, so replace it rather than mutate it.
type Module struct {
	Name string
	Raw  json.RawMessage
}

// Genesis is an AppGenesis whose app_state is held as modules sorted by name.
type Genesis struct {
	AppName       string
	AppVersion    string
	GenesisTime   time.Time
	ChainID       string
	InitialHeight int64
	AppHash       []byte
	Modules       []Module
	Consensus     *genutiltypes.ConsensusGenesis
}

// Module returns the payload of the named module.
func (g *Genesis) Module(name string) (json.RawMessage, bool) {
	i, ok := slices.BinarySearchFunc(g.Modules, name, func(m Module, name string) int {
		return strings.Compare(m.Name, name)
	})
	if !ok {
		return nil, false
	}
	return g.Modules[i].Raw, true
}

// Export is a loaded export file and the SHA-256 of its bytes.
type Export struct {
	Genesis
	SHA256 [sha256.Size]byte
}

// Options are the operator inputs for building a candidate.
type Options struct {
	// ExportSHA256 is the expected hex SHA-256 of the export file.
	ExportSHA256 string
	// ExportHeight is the last executed height E of the exported chain.
	ExportHeight int64
	// ExportBlockTime is the block time T at ExportHeight.
	ExportBlockTime time.Time
	// GenesisTime is the restart time G.
	GenesisTime time.Time
	// InitialHeight is the first height of the restarted chain.
	InitialHeight int64
	// ChainID is the output chain-id; empty keeps the export's.
	ChainID string
}

// CheckError reports the first failed check.
type CheckError struct {
	Step   string
	Module string
	Field  string
	Want   string
	Got    string
}

// maxErrorValue caps the source bytes of each value shown by CheckError.Error.
const maxErrorValue = 128

func (e *CheckError) Error() string {
	var b strings.Builder
	b.WriteString(e.Step)
	for _, s := range []string{e.Module, e.Field} {
		if s != "" {
			b.WriteString(": " + quote(s))
		}
	}
	if e.Want == "" {
		return b.String() + ": " + quote(e.Got)
	}
	return fmt.Sprintf("%s: want %s, got %s", b.String(), quote(e.Want), quote(e.Got))
}

// quote escapes s to printable ASCII, cut to maxErrorValue source bytes at a rune boundary.
func quote(s string) string {
	if len(s) <= maxErrorValue {
		return strconv.QuoteToASCII(s)
	}
	cut := maxErrorValue
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strconv.QuoteToASCII(s[:cut]) + "..."
}

// ExportedModules returns the names of the modules that the export writes into app_state.
func ExportedModules(mm *module.Manager) []string {
	names := make([]string, 0, len(mm.OrderExportGenesis))
	for _, name := range mm.OrderExportGenesis {
		// Same type switch as module.Manager.ExportGenesisForModules.
		switch mm.Modules[name].(type) {
		case appmodule.HasGenesis, module.HasGenesis, module.HasABCIGenesis:
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}
