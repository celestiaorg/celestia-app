package genesis

import (
	"encoding/json"
	"time"

	"github.com/celestiaorg/celestia-app/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// FromDocument converts a genesis document to a genesis struct. This is meant
// to allow for the testing an existing genesis document. It does not support a
// genesis that has existing validators or is starting at a height other 0. Not
// all genesis state is copied to the new testenet genesis. Only state from the
// bank and auth modules are copied.
func FromDocument(gdoc *coretypes.GenesisDoc) (*Genesis, error) {
	var appState map[string]json.RawMessage
	err := json.Unmarshal(gdoc.AppState, &appState)
	if err != nil {
		return nil, err
	}

	g := NewDefaultGenesis().
		WithChainID(gdoc.ChainID).
		// replace the genesis time so that the testnet can begin to produce
		// blocks
		WithGenesisTime(time.Now()).
		WithConsensusParams(gdoc.ConsensusParams)

	g = g.WithModifiers(
		combineAuthState(g.ecfg, appState),
		combineBankState(g.ecfg, appState),
	)

	// migrate any genesis transactions from the genesis doc to the testnet
	// genesis
	var genutilState genutiltypes.GenesisState
	g.ecfg.Codec.MustUnmarshalJSON(appState[genutiltypes.ModuleName], &genutilState)

	var genTxs []sdk.Tx
	for _, gtx := range genutilState.GenTxs {
		sdkTx, err := g.ecfg.TxConfig.TxJSONDecoder()(gtx)
		if err != nil {
			return nil, err
		}
		genTxs = append(genTxs, sdkTx)
	}

	g = g.WithGenTx(genTxs...)

	return g, nil
}

// combineAuthState combines the auth state in the provided genesis state with
// the auth state that the genesis modifier is applied to.
func combineAuthState(ecfg encoding.Config, docAppState map[string]json.RawMessage) Modifier {
	return func(genesisState map[string]json.RawMessage) map[string]json.RawMessage {
		var (
			newAuthState, oldAuthState authtypes.GenesisState
		)

		ecfg.Codec.MustUnmarshalJSON(genesisState[authtypes.ModuleName], &newAuthState)
		ecfg.Codec.MustUnmarshalJSON(docAppState[authtypes.ModuleName], &oldAuthState)
		newAuthState.Accounts = append(newAuthState.Accounts, oldAuthState.Accounts...)
		genesisState[authtypes.ModuleName] = ecfg.Codec.MustMarshalJSON(&newAuthState)

		return genesisState
	}
}

// combineBankState combines the bank state in the provided genesis state with
// the bank state that the genesis modifier is applied to.
func combineBankState(ecfg encoding.Config, docAppState map[string]json.RawMessage) Modifier {
	return func(genesisState map[string]json.RawMessage) map[string]json.RawMessage {
		var (
			newBankState, oldBankState banktypes.GenesisState
		)

		ecfg.Codec.MustUnmarshalJSON(genesisState[banktypes.ModuleName], &newBankState)
		ecfg.Codec.MustUnmarshalJSON(docAppState[banktypes.ModuleName], &oldBankState)
		newBankState.Balances = append(newBankState.Balances, oldBankState.Balances...)

		// calculate the new supply by adding up all balances in both sets of state
		totalSupply := sdk.NewCoins()
		for _, bal := range newBankState.Balances {
			totalSupply = totalSupply.Add(sdk.NewCoins(bal.Coins...)...)
		}

		newBankState.Supply = totalSupply
		genesisState[banktypes.ModuleName] = ecfg.Codec.MustMarshalJSON(&newBankState)
		return genesisState
	}
}
