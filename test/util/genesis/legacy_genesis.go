package genesis

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	maps "github.com/celestiaorg/celestia-app/v6/internal/utils"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

// DocumentLegacyBytes generates a legacy genesis document as bytes by combining various input configurations and state data.
// It handles the conversion of account data into legacy SDK types and ensures compatibility with older genesis format versions.
// Ensures proper initialization of consensus parameters, app state, and genesis document configuration.
// Directly manipulates JSON bytes to restore fields needed for legacy compatibility during the marshalling process.
// Returns the serialized genesis document as raw bytes or an error if any step fails during processing.
func DocumentLegacyBytes(
	defaultGenesis map[string]json.RawMessage,
	ecfg encoding.Config,
	params *tmproto.ConsensusParams,
	chainID string,
	gentxs []json.RawMessage,
	accounts []Account,
	genesisTime time.Time,
) ([]byte, error) {
	genutilGenState := genutiltypes.DefaultGenesisState()
	genutilGenState.GenTxs = gentxs

	genBals, genAccs, err := accountsToSDKTypes(accounts)
	if err != nil {
		return nil, fmt.Errorf("converting accounts into sdk types: %w", err)
	}

	sdkAccounts, err := authtypes.PackAccounts(genAccs)
	if err != nil {
		return nil, fmt.Errorf("packing accounts: %w", err)
	}

	authGenState := authtypes.DefaultGenesisState()
	authGenState.Accounts = append(authGenState.Accounts, sdkAccounts...)

	state := defaultGenesis
	state[authtypes.ModuleName] = ecfg.Codec.MustMarshalJSON(authGenState)
	state[banktypes.ModuleName] = getLegacyBankState(genBals)
	state[genutiltypes.ModuleName] = ecfg.Codec.MustMarshalJSON(genutilGenState)

	appStateBz, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling genesis state: %w", err)
	}

	cp := coretypes.ConsensusParamsFromProto(*params)

	genesisDoc := &coretypes.GenesisDoc{
		ChainID:         chainID,
		AppState:        appStateBz,
		ConsensusParams: &cp,
		GenesisTime:     genesisTime,
	}

	bz, err := cmtjson.Marshal(genesisDoc)
	if err != nil {
		return nil, fmt.Errorf("marshalling genesis doc: %w", err)
	}

	// we need to add back this field that has been removed between v3 and v4.
	// we manipulate the bytes directly to achieve this.
	bz, err = maps.SetField(bz, "consensus_params.block.time_iota_ms", "1000")
	if err != nil {
		return nil, fmt.Errorf("adding time_iota_ms field: %w", err)
	}

	// the version field used to be at this consensus_params.version.app_version
	bz, err = maps.SetField(bz, "consensus_params.version.app_version", strconv.Itoa(int(cp.Version.App)))
	if err != nil {
		return nil, fmt.Errorf("adding app_version field: %w", err)
	}

	// remove the version from the new location.
	bz, err = maps.RemoveField(bz, "consensus_params.version.app")
	if err != nil {
		return nil, fmt.Errorf("removing version.app field: %w", err)
	}

	return bz, nil
}

// getLegacyBankState returns valid bytes for a pre v4 bank genesis appstate.
func getLegacyBankState(genBals []banktypes.Balance) []byte {
	bankGenState := banktypes.DefaultGenesisState()
	bankGenState.Balances = append(bankGenState.Balances, genBals...)
	bankGenState.Balances = banktypes.SanitizeGenesisBalances(bankGenState.Balances)

	// the SendEnabled field was moved from Params.SendEnabled to the top level.
	// we need to re-vert this change in order to convert a v4 genesis app state to a valid v3 genesis app state.
	bankGenState.Params.SendEnabled = make([]*banktypes.SendEnabled, 0) //nolint:staticcheck
	for _, se := range bankGenState.SendEnabled {
		bankGenState.Params.SendEnabled = append(bankGenState.Params.SendEnabled, &se) //nolint:staticcheck
	}
	bankGenState.SendEnabled = nil

	bz, err := json.Marshal(bankGenState)
	if err != nil {
		panic(err)
	}

	// "send_enabled" does not have the `omitempty` tag, so we need to remove it.
	withoutSendEnabled, err := maps.RemoveField(bz, "send_enabled")
	if err != nil {
		panic(err)
	}

	return withoutSendEnabled
}

// loadV3GenesisAppState reads and unmarshals the v3 genesis app state from a predefined JSON file path.
func loadV3GenesisAppState() map[string]json.RawMessage {
	// NOTE: when e2e tests are _test.go files again, this can be loaded with just v3_genesis_app_state.json
	// as it will be in n test file directory.
	const v3GenesisAppStateFilePath = "test/e2e/test_data/v3genesisAppState.json"
	file, err := os.Open(v3GenesisAppStateFilePath)
	if err != nil {
		panic(fmt.Errorf("failed to open file: %w", err))
	}
	defer file.Close()

	bz, err := io.ReadAll(file)
	if err != nil {
		panic(fmt.Errorf("failed to read file: %w", err))
	}

	var appState map[string]json.RawMessage
	if err := json.Unmarshal(bz, &appState); err != nil {
		panic(fmt.Errorf("failed to unmarshal default app state: %w", err))
	}

	return appState
}
