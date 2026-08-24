package hardspoon

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"time"

	"cosmossdk.io/math"
	circuittypes "cosmossdk.io/x/circuit/types"
	feegranttypes "cosmossdk.io/x/feegrant"
	blobtypes "github.com/celestiaorg/celestia-app/v9/x/blob/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v9/x/minfee/types"
	cmttypes "github.com/cometbft/cometbft/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	icagenesistypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/genesis/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

// carriedVerbatim are the modules whose exported genesis is copied across
// unchanged. Each is either pure parameters or state that stays valid on a chain
// with no history.
var carriedVerbatim = []string{
	minttypes.ModuleName,     // bond_denom only; the inflation clock restarts by design
	blobtypes.ModuleName,     // gas_per_blob_byte, gov_max_square_size
	authztypes.ModuleName,    // grants stay valid; expired ones prune themselves
	feegranttypes.ModuleName, // allowances stay valid
	circuittypes.ModuleName,  // disabled message types and account permissions
}

// assemble builds the new genesis document.
//
// Everything starts from the current binary's default genesis, so the modules
// being dropped (ibc, capability, hyperlane, warp, zkism, evidence, upgrade,
// genutil) are correct by construction rather than by being emptied field by
// field. Only what is listed here is carried over.
func (s *spoon) assemble(defaultGenesis map[string]json.RawMessage) (*cmttypes.GenesisDoc, error) {
	appState := make(map[string]json.RawMessage, len(defaultGenesis))
	maps.Copy(appState, defaultGenesis)

	for _, module := range carriedVerbatim {
		raw, ok := s.fork.AppState[module]
		if !ok {
			return nil, fmt.Errorf("export is missing the %q module", module)
		}
		appState[module] = raw
	}

	balances, supply := s.balancesAndSupply()

	packed, err := packAccounts(s.accounts)
	if err != nil {
		return nil, err
	}

	// auth: parameters and the rebuilt account list.
	auth := authtypes.GenesisState{Params: s.auth.Params, Accounts: packed}

	// bank: parameters, denom metadata and send_enabled carried; balances
	// rebuilt; supply recomputed. bank's InitGenesis panics if the stated supply
	// disagrees with the sum of balances, so it has to be derived, not carried.
	metadata, droppedMetadata := carriedDenomMetadata(s.bank.DenomMetadata)
	s.report.DenomMetadataDropped = droppedMetadata
	// A send_enabled override for a dropped denom would govern coins that no
	// longer exist, so it goes with them. mocha-4 carries none.
	sendEnabled := make([]banktypes.SendEnabled, 0, len(s.bank.SendEnabled))
	for _, entry := range s.bank.SendEnabled {
		if orphanedDenom(entry.Denom) {
			continue
		}
		sendEnabled = append(sendEnabled, entry)
	}
	bank := banktypes.GenesisState{
		Params:        s.bank.Params,
		Balances:      balances,
		Supply:        supply,
		DenomMetadata: metadata,
		SendEnabled:   sendEnabled,
	}

	// staking: parameters only. Every token that was staked or unbonding has
	// been credited back to its owner as liquid balance, so nobody is staked and
	// the genesis validator set comes from gentxs collected later.
	staking := stakingtypes.GenesisState{
		Params:         s.staking.Params,
		LastTotalPower: math.ZeroInt(),
		Exported:       false,
	}

	// distribution: parameters only. Rewards and commissions are already in
	// balances, and the community pool is deliberately not carried.
	distribution := distributiontypes.GenesisState{
		Params:  s.distribution.Params,
		FeePool: distributiontypes.InitialFeePool(),
	}

	// slashing: parameters only. Jailing and tombstone history is forgiven by
	// construction, since there are no validators to carry it for.
	slashing := slashingtypes.GenesisState{Params: s.slashing.Params}

	// gov: parameters only. Proposals are dropped and the counter restarts.
	// Deposits are gone with the gov module account; nothing in the export
	// refunds them, and the export this was built against had none.
	govParams, err := alignExpeditedVotingPeriod(s.gov.Params, s.report)
	if err != nil {
		return nil, err
	}
	gov := govv1.GenesisState{
		StartingProposalId: 1,
		Params:             govParams,
		Constitution:       s.gov.Constitution,
	}

	// minfee: the export only fills the deprecated top-level field while
	// InitGenesis reads only params, so params would silently fall back to the
	// default. Write both from the exported value. ValidateGenesis still
	// requires the deprecated field to be set.
	if s.minfee.NetworkMinGasPrice.IsNil() || !s.minfee.NetworkMinGasPrice.IsPositive() {
		return nil, fmt.Errorf(
			"exported minfee network_min_gas_price is %v; expected a positive value",
			s.minfee.NetworkMinGasPrice,
		)
	}
	minfee := minfeetypes.GenesisState{
		NetworkMinGasPrice: s.minfee.NetworkMinGasPrice,
		Params:             minfeetypes.Params{NetworkMinGasPrice: s.minfee.NetworkMinGasPrice},
	}

	// transfer: parameters only. Voucher balances are dropped as orphaned
	// denoms, since the channels they could be redeemed through do not survive,
	// so the traces that named them go too. The escrow accounting resets because
	// the escrow balances themselves are gone.
	transfer := ibctransfertypes.GenesisState{
		PortId: s.transfer.PortId,
		Params: s.transfer.Params,
	}

	// interchainaccounts: parameters only. Registered accounts and active
	// channels die with the ibc state.
	ica := icagenesistypes.GenesisState{
		ControllerGenesisState: icagenesistypes.ControllerGenesisState{
			Params: s.ica.ControllerGenesisState.Params,
		},
		HostGenesisState: icagenesistypes.HostGenesisState{
			Port:   s.ica.HostGenesisState.Port,
			Params: s.ica.HostGenesisState.Params,
		},
	}

	typed := []struct {
		module string
		state  proto.Message
	}{
		{authtypes.ModuleName, &auth},
		{banktypes.ModuleName, &bank},
		{stakingtypes.ModuleName, &staking},
		{distributiontypes.ModuleName, &distribution},
		{slashingtypes.ModuleName, &slashing},
		{govtypes.ModuleName, &gov},
		{minfeetypes.ModuleName, &minfee},
		{ibctransfertypes.ModuleName, &transfer},
		{icatypes.ModuleName, &ica},
	}
	for _, entry := range typed {
		raw, err := s.cdc.MarshalJSON(entry.state)
		if err != nil {
			return nil, fmt.Errorf("marshaling %q genesis: %w", entry.module, err)
		}
		appState[entry.module] = raw
	}

	// Run every module's own ValidateGenesis while the app state is still in
	// hand. This is what turns state that is legal on a live chain but illegal in
	// a genesis file into a failure here, naming the module and the offending
	// record, rather than an opaque error out of a later genesis subcommand.
	if s.opts.ValidateGenesis != nil {
		if err := s.opts.ValidateGenesis(appState); err != nil {
			return nil, fmt.Errorf("assembled genesis is not valid: %w", err)
		}
	}

	appStateBytes, err := json.Marshal(appState)
	if err != nil {
		return nil, fmt.Errorf("marshaling app state: %w", err)
	}

	s.report.Out = Totals{
		Accounts: len(s.accounts),
		Balances: len(balances),
		Supply:   supply,
	}

	return &cmttypes.GenesisDoc{
		GenesisTime:     s.opts.GenesisTime.UTC(),
		ChainID:         s.opts.ChainID,
		InitialHeight:   s.opts.InitialHeight,
		ConsensusParams: s.consensusParams(),
		AppState:        appStateBytes,
	}, nil
}

// consensusParams carries the exported consensus params and sets the two fields
// the export drops.
//
// `export` rebuilds consensus params through genutil's NewConsensusGenesis,
// which only copies block, evidence and validator. Version and ABCI are left at
// their zero values, so an exported genesis claims app version 0 and would start
// the new chain on the wrong state machine.
func (s *spoon) consensusParams() *cmttypes.ConsensusParams {
	params := *s.fork.ConsensusParams
	params.Version = cmttypes.VersionParams{App: s.opts.AppVersion}
	params.ABCI = cmttypes.ABCIParams{VoteExtensionsEnableHeight: 0}
	return &params
}

// balancesAndSupply renders the balance ledger in address order and derives the
// total supply from it.
func (s *spoon) balancesAndSupply() ([]banktypes.Balance, sdk.Coins) {
	addresses := make([]string, 0, len(s.balances))
	for address, coins := range s.balances {
		if coins.IsZero() {
			continue
		}
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)

	balances := make([]banktypes.Balance, 0, len(addresses))
	supply := sdk.NewCoins()
	for _, address := range addresses {
		coins := s.balances[address]
		balances = append(balances, banktypes.Balance{Address: address, Coins: coins})
		supply = supply.Add(coins...)
	}
	return balances, supply
}

// alignExpeditedVotingPeriod returns the carried gov params with the expedited
// voting period shortened by one second, if it was not already strictly shorter
// than the regular voting period.
//
// gov requires the expedited period to be strictly shorter, and a live chain can
// hold a pair that is not. arabica-11 does, with both at 24h: governance shortened
// voting_period from the SDK's 48h default, and expedited_voting_period arrived
// later at DefaultExpeditedPeriod when the field was introduced, with nothing
// re-checking the pair afterwards. MsgUpdateParams validates, so the combination
// can only have come in through that migration.
//
// One second is deliberately the entire adjustment. A larger cut would be a
// decision about how fast governance may move on the new chain: with the periods
// equal, an expedited proposal buys no time on the old chain, only a higher
// threshold and a higher deposit, and shortening the period would change that.
// The comparison mirrors ValidateBasic's own, on seconds rather than on the
// durations, so the two cannot disagree.
func alignExpeditedVotingPeriod(params *govv1.Params, report *Report) (*govv1.Params, error) {
	if params == nil || params.VotingPeriod == nil || params.ExpeditedVotingPeriod == nil {
		return params, nil
	}
	if params.ExpeditedVotingPeriod.Seconds() < params.VotingPeriod.Seconds() {
		return params, nil
	}

	shortened := *params.VotingPeriod - time.Second
	if shortened.Seconds() <= 0 {
		return nil, fmt.Errorf(
			"exported gov voting period is %s and the expedited period is %s, which has to be "+
				"strictly shorter; there is no positive value below the regular period to give it",
			params.VotingPeriod, params.ExpeditedVotingPeriod,
		)
	}

	report.GovExpeditedVotingPeriod = &PeriodAdjustment{
		From: params.ExpeditedVotingPeriod.String(),
		To:   shortened.String(),
	}

	// Copied rather than written through: the loaded genesis state is shared, and
	// the durations behind it are pointers.
	aligned := *params
	aligned.ExpeditedVotingPeriod = &shortened
	return &aligned, nil
}

// carriedDenomMetadata drops denom metadata that bank's own validation rejects
// or that names an orphaned denom, returning the surviving entries and the base
// denoms of the dropped ones.
//
// ibc-go's SetDenomMetadata writes the trace's base denom as the first denom unit
// while the metadata's base is the IBC hash, and Metadata.Validate requires the
// two to match, so every voucher a chain has ever received carries an entry that
// cannot go into a genesis file. mocha-4 has one. Metadata for an orphaned denom
// would label coins that no longer exist. Metadata is display-only either way, so
// dropping it costs a label in wallets and keeps the genesis loadable.
func carriedDenomMetadata(metadata []banktypes.Metadata) (kept []banktypes.Metadata, dropped []string) {
	kept = make([]banktypes.Metadata, 0, len(metadata))
	for _, entry := range metadata {
		if err := entry.Validate(); err != nil || orphanedDenom(entry.Base) {
			dropped = append(dropped, entry.Base)
			continue
		}
		kept = append(kept, entry)
	}
	return kept, dropped
}

// packAccounts converts the working account list back into the Any-wrapped form
// auth's genesis uses.
func packAccounts(accounts []sdk.AccountI) ([]*codectypes.Any, error) {
	genesisAccounts := make(authtypes.GenesisAccounts, 0, len(accounts))
	for _, account := range accounts {
		genesisAccount, ok := account.(authtypes.GenesisAccount)
		if !ok {
			return nil, fmt.Errorf("account %s is not a genesis account type", account.GetAddress())
		}
		genesisAccounts = append(genesisAccounts, genesisAccount)
	}
	packed, err := authtypes.PackAccounts(genesisAccounts)
	if err != nil {
		return nil, fmt.Errorf("packing accounts: %w", err)
	}
	return packed, nil
}
