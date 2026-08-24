package hardspoon

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v9/x/minfee/types"
	zkismtypes "github.com/celestiaorg/celestia-app/v9/x/zkism/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

// Verify re-checks every invariant a spoon is supposed to establish.
//
// It runs at the end of Transform and is also exposed as a subcommand, because
// the genesis is rewritten twice on its way to being published: collect-gentxs
// re-indents it and converts it to the SDK's layout, and convert-genesis
// converts it back. Running this on the final file is what proves those
// round-trips did not lose the app version or break bank/auth parity.
func Verify(cdc codec.Codec, result *Result, opts Options) error {
	var problems []error
	note := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf(format, args...))
	}

	genesis := result.Genesis

	if genesis.ChainID != opts.ChainID {
		note("chain id is %q, want %q", genesis.ChainID, opts.ChainID)
	}
	if genesis.InitialHeight != opts.InitialHeight {
		note("initial height is %d, want %d", genesis.InitialHeight, opts.InitialHeight)
	}
	if !genesis.GenesisTime.Equal(opts.GenesisTime) {
		note("genesis time is %s, want %s", genesis.GenesisTime, opts.GenesisTime)
	}
	if genesis.ConsensusParams == nil {
		note("consensus params are missing")
	} else if got := genesis.ConsensusParams.Version.App; got != opts.AppVersion {
		// This is the one the export gets wrong, so it is worth stating loudly:
		// starting on the wrong app version means the wrong state machine.
		note("consensus params app version is %d, want %d", got, opts.AppVersion)
	}
	if len(genesis.Validators) != 0 {
		note("genesis has %d validators; the set is meant to come from gentxs", len(genesis.Validators))
	}

	if result.Bytes == nil {
		note("genesis was not serialized")
	} else if max := opts.MaxSizeBytes; max > 0 && len(result.Bytes) > max {
		note(
			"serialized genesis is %d bytes, over the %d byte cap. Multiplexer nodes replaying "+
				"this chain from genesis run InitChain over gRPC against an embedded binary with a "+
				"75 MiB receive limit, so they could never sync",
			len(result.Bytes), max,
		)
	}

	var appState map[string]json.RawMessage
	if err := json.Unmarshal(genesis.AppState, &appState); err != nil {
		return fmt.Errorf("app state is not readable: %w", err)
	}

	problems = append(problems, verifyAccountsAndBalances(cdc, appState, opts.GenesisTime)...)
	problems = append(problems, verifyEmptied(cdc, appState)...)
	problems = append(problems, verifyMinFee(cdc, appState)...)

	return errors.Join(problems...)
}

// verifyAccountsAndBalances checks the auth and bank states agree.
func verifyAccountsAndBalances(cdc codec.Codec, appState map[string]json.RawMessage, genesisTime time.Time) []error {
	var problems []error
	note := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf(format, args...))
	}

	var auth authtypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[authtypes.ModuleName], &auth); err != nil {
		return []error{fmt.Errorf("reading auth genesis: %w", err)}
	}
	var bank banktypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[banktypes.ModuleName], &bank); err != nil {
		return []error{fmt.Errorf("reading bank genesis: %w", err)}
	}

	accounts, err := authtypes.UnpackAccounts(auth.Accounts)
	if err != nil {
		return []error{fmt.Errorf("unpacking accounts: %w", err)}
	}

	addresses := make(map[string]struct{}, len(accounts))
	for i, account := range accounts {
		address := account.GetAddress().String()
		if _, ok := addresses[address]; ok {
			note("duplicate account %s", address)
		}
		addresses[address] = struct{}{}

		if account.GetSequence() != 0 {
			note("account %s has sequence %d, want 0", address, account.GetSequence())
		}
		if got := account.GetAccountNumber(); got != uint64(i) {
			note("account %s has number %d, want %d (numbers must be contiguous from 0)", address, got, i)
		}
		if module, ok := account.(*authtypes.ModuleAccount); ok {
			note("module account %q survived the strip", module.Name)
		}

		// Flattening removes every schedule that had already elapsed. One that
		// survived is inert state a genesis file happens to accept, so nothing
		// else here or in ValidateGenesis would notice it.
		if base := baseVesting(account); base != nil && !genesisTime.IsZero() {
			if _, permanent := account.(*vestingtypes.PermanentLockedAccount); !permanent {
				if base.EndTime != 0 && base.EndTime <= genesisTime.Unix() {
					note(
						"vesting account %s ends at %d, at or before the genesis time %d; "+
							"an elapsed schedule locks nothing and should have been flattened",
						address, base.EndTime, genesisTime.Unix(),
					)
				}
			}
		}
	}

	// Every balance needs an account. The converse does not hold: a vesting
	// account is kept even when it holds nothing, because its schedule still has
	// to reject early spends.
	seen := make(map[string]struct{}, len(bank.Balances))
	supply := sdk.NewCoins()
	for _, balance := range bank.Balances {
		if _, ok := seen[balance.Address]; ok {
			note("duplicate balance for %s", balance.Address)
		}
		seen[balance.Address] = struct{}{}

		if _, ok := addresses[balance.Address]; !ok {
			note("balance for %s has no account", balance.Address)
		}
		if balance.Coins.IsZero() {
			note("balance for %s is zero and should have been dropped", balance.Address)
		}
		// The modules that could redeem bridged assets do not survive the spoon,
		// so neither may their coins: they would be spendable but redeemable for
		// nothing.
		for _, coin := range balance.Coins {
			if orphanedDenom(coin.Denom) {
				note("balance for %s holds %s, an orphaned denom that should have been dropped",
					balance.Address, coin)
			}
		}
		supply = supply.Add(balance.Coins...)
	}

	// bank's InitGenesis panics on a mismatch here, which would stop the chain
	// from starting at all.
	if !supply.Equal(bank.Supply) {
		note("stated supply %s does not equal the sum of balances %s", bank.Supply, supply)
	}

	for _, coin := range bank.Supply {
		if orphanedDenom(coin.Denom) {
			note("supply carries %s, an orphaned denom that should have been dropped", coin)
		}
	}
	for _, entry := range bank.SendEnabled {
		if orphanedDenom(entry.Denom) {
			note("send_enabled entry for orphaned denom %s should not have been carried", entry.Denom)
		}
	}

	// Every voucher a chain has received leaves behind denom metadata that
	// ibc-go wrote and bank's own validation rejects, so a surviving invalid
	// entry means the whole genesis fails ValidateGenesis.
	for _, entry := range bank.DenomMetadata {
		if err := entry.Validate(); err != nil {
			note("denom metadata for %s is invalid and should have been dropped: %v", entry.Base, err)
		}
		if orphanedDenom(entry.Base) {
			note("denom metadata for orphaned denom %s should not have been carried", entry.Base)
		}
	}

	return problems
}

// verifyEmptied checks the modules that must not carry history.
func verifyEmptied(cdc codec.Codec, appState map[string]json.RawMessage) []error {
	var problems []error
	note := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf(format, args...))
	}

	var staking stakingtypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[stakingtypes.ModuleName], &staking); err != nil {
		return []error{fmt.Errorf("reading staking genesis: %w", err)}
	}
	// staking's InitGenesis asserts each pool's balance equals its derived
	// tokens, so any leftover validator or delegation here would panic against
	// the emptied pools.
	if n := len(staking.Validators); n != 0 {
		note("staking genesis has %d validators", n)
	}
	if n := len(staking.Delegations); n != 0 {
		note("staking genesis has %d delegations", n)
	}
	if n := len(staking.UnbondingDelegations); n != 0 {
		note("staking genesis has %d unbonding delegations", n)
	}
	if n := len(staking.Redelegations); n != 0 {
		note("staking genesis has %d redelegations", n)
	}
	if !staking.LastTotalPower.IsNil() && !staking.LastTotalPower.IsZero() {
		note("staking last_total_power is %s, want 0", staking.LastTotalPower)
	}
	if staking.Exported {
		note("staking genesis is still flagged as exported")
	}

	var distribution distributiontypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[distributiontypes.ModuleName], &distribution); err != nil {
		return []error{fmt.Errorf("reading distribution genesis: %w", err)}
	}
	if n := len(distribution.DelegatorStartingInfos); n != 0 {
		note("distribution genesis has %d delegator starting infos", n)
	}
	if n := len(distribution.ValidatorSlashEvents); n != 0 {
		note("distribution genesis has %d validator slash events", n)
	}
	if !distribution.FeePool.CommunityPool.IsZero() {
		note("community pool is %s, want empty", distribution.FeePool.CommunityPool)
	}

	var slashing slashingtypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[slashingtypes.ModuleName], &slashing); err != nil {
		return []error{fmt.Errorf("reading slashing genesis: %w", err)}
	}
	if n := len(slashing.SigningInfos); n != 0 {
		note("slashing genesis has %d signing infos", n)
	}
	if n := len(slashing.MissedBlocks); n != 0 {
		note("slashing genesis has %d missed block records", n)
	}

	var gov govv1.GenesisState
	if err := cdc.UnmarshalJSON(appState[govtypes.ModuleName], &gov); err != nil {
		return []error{fmt.Errorf("reading gov genesis: %w", err)}
	}
	if n := len(gov.Proposals); n != 0 {
		note("gov genesis has %d proposals", n)
	}
	if n := len(gov.Deposits); n != 0 {
		note("gov genesis has %d deposits", n)
	}
	if n := len(gov.Votes); n != 0 {
		note("gov genesis has %d votes", n)
	}
	if gov.StartingProposalId != 1 {
		note("gov starting_proposal_id is %d, want 1", gov.StartingProposalId)
	}
	// Delegated to gov's own validation rather than restated, so it cannot drift
	// from it. The carried params are constrained more tightly by a genesis file
	// than by a live chain: arabica-11 runs with the expedited and regular voting
	// periods equal, which ValidateBasic rejects.
	if gov.Params != nil {
		if err := gov.Params.ValidateBasic(); err != nil {
			note("gov params are invalid: %v", err)
		}
	}

	// Dropping the hyperlane/... coins is only sound because the modules that
	// minted them restart empty, so verify pins both sides of that.
	var hyperlane hyperlanetypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[hyperlanetypes.ModuleName], &hyperlane); err != nil {
		return append(problems, fmt.Errorf("reading hyperlane genesis: %w", err))
	}
	if n := len(hyperlane.Mailboxes); n != 0 {
		note("hyperlane genesis has %d mailboxes", n)
	}
	if n := len(hyperlane.Messages); n != 0 {
		note("hyperlane genesis has %d delivered messages", n)
	}
	if hyperlane.IsmSequence != 0 || hyperlane.PostDispatchSequence != 0 || hyperlane.AppSequence != 0 {
		note("hyperlane sequences are %d/%d/%d, want 0",
			hyperlane.IsmSequence, hyperlane.PostDispatchSequence, hyperlane.AppSequence)
	}
	if g := hyperlane.IsmGenesis; g != nil && len(g.Isms) != 0 {
		note("hyperlane genesis has %d ISMs", len(g.Isms))
	}
	if g := hyperlane.PostDispatchGenesis; g != nil {
		if n := len(g.Igps) + len(g.IgpGasConfigs) + len(g.MerkleTreeHooks) + len(g.NoopHooks); n != 0 {
			note("hyperlane genesis has %d post-dispatch hooks and gas configs", n)
		}
	}

	var warp warptypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[warptypes.ModuleName], &warp); err != nil {
		return append(problems, fmt.Errorf("reading warp genesis: %w", err))
	}
	if n := len(warp.Tokens); n != 0 {
		note("warp genesis has %d token registrations", n)
	}
	if n := len(warp.RemoteRouters); n != 0 {
		note("warp genesis has %d remote routers", n)
	}

	// transfer: a carried denom trace would name a voucher that no longer
	// exists, and escrow accounting would claim funds the dropped escrow
	// accounts no longer hold.
	var transfer ibctransfertypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[ibctransfertypes.ModuleName], &transfer); err != nil {
		return append(problems, fmt.Errorf("reading transfer genesis: %w", err))
	}
	if n := len(transfer.DenomTraces); n != 0 {
		note("transfer genesis has %d denom traces; vouchers do not survive the spoon", n)
	}
	if !transfer.TotalEscrowed.IsZero() {
		note("transfer total_escrowed is %s, want empty", transfer.TotalEscrowed)
	}

	var zkism zkismtypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[zkismtypes.ModuleName], &zkism); err != nil {
		return append(problems, fmt.Errorf("reading zkism genesis: %w", err))
	}
	if n := len(zkism.Isms); n != 0 {
		note("zkism genesis has %d ISMs", n)
	}
	if n := len(zkism.Messages); n != 0 {
		note("zkism genesis has %d authorized messages", n)
	}
	if n := len(zkism.Submissions); n != 0 {
		note("zkism genesis has %d proof submissions", n)
	}

	return problems
}

// verifyMinFee checks the workaround for the export's minfee bug took effect.
func verifyMinFee(cdc codec.Codec, appState map[string]json.RawMessage) []error {
	var minfee minfeetypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[minfeetypes.ModuleName], &minfee); err != nil {
		return []error{fmt.Errorf("reading minfee genesis: %w", err)}
	}

	var problems []error
	if minfee.Params.NetworkMinGasPrice.IsNil() || !minfee.Params.NetworkMinGasPrice.IsPositive() {
		problems = append(problems, fmt.Errorf(
			"minfee params.network_min_gas_price is %v; InitGenesis reads only this field",
			minfee.Params.NetworkMinGasPrice,
		))
	}
	// ValidateGenesis rejects a zero value in the deprecated field, so it has to
	// be set even though nothing reads it.
	if minfee.NetworkMinGasPrice.IsNil() || !minfee.NetworkMinGasPrice.IsPositive() {
		problems = append(problems, fmt.Errorf(
			"minfee network_min_gas_price is %v; ValidateGenesis requires it to be positive",
			minfee.NetworkMinGasPrice,
		))
	}
	if len(problems) == 0 && !minfee.Params.NetworkMinGasPrice.Equal(minfee.NetworkMinGasPrice) {
		problems = append(problems, fmt.Errorf(
			"minfee params.network_min_gas_price %s does not match network_min_gas_price %s",
			minfee.Params.NetworkMinGasPrice, minfee.NetworkMinGasPrice,
		))
	}
	return problems
}

// VerifyFile re-runs Verify against a genesis document already on disk.
func VerifyFile(cdc codec.Codec, raw []byte, opts Options) (*cmttypes.GenesisDoc, error) {
	fork, err := LoadFork(raw)
	if err != nil {
		return nil, err
	}

	var appState json.RawMessage
	appState, err = json.Marshal(fork.AppState)
	if err != nil {
		return nil, err
	}

	genesis := &cmttypes.GenesisDoc{
		ChainID:         fork.ChainID,
		InitialHeight:   fork.InitialHeight,
		ConsensusParams: fork.ConsensusParams,
		AppState:        appState,
	}
	// Only the fields the caller pinned are compared; the rest are read back
	// from the file itself.
	if opts.ChainID == "" {
		opts.ChainID = genesis.ChainID
	}
	if opts.InitialHeight == 0 {
		opts.InitialHeight = genesis.InitialHeight
	}

	var genesisTime struct {
		GenesisTime string `json:"genesis_time"`
	}
	if err := json.Unmarshal(raw, &genesisTime); err == nil && genesisTime.GenesisTime != "" {
		if parsed, err := cmttypes.GenesisDocFromJSON(raw); err == nil {
			genesis.GenesisTime = parsed.GenesisTime
		}
	}
	opts.GenesisTime = genesis.GenesisTime

	return genesis, Verify(cdc, &Result{Genesis: genesis, Bytes: raw}, opts)
}
