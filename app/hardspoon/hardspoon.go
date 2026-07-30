// Package hardspoon rebuilds a new chain's genesis from an
// `export --for-zero-height` snapshot of an old one.
//
// Every delegation, unbonding delegation and accrued staking reward is redeemed
// to the owning account as liquid tokens, module-account funds and provably
// unreachable balances are dropped, and the modules that governance can tune are
// carried across verbatim so the new chain is a parameter-level replica of the
// old one. Everything else starts from the current binary's default genesis, so
// wiped modules are consistent by construction and carried state is an explicit
// allowlist.
package hardspoon

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/math"
	minfeetypes "github.com/celestiaorg/celestia-app/v9/x/minfee/types"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	icagenesistypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/genesis/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

// DefaultMaxSizeBytes is the ceiling hardspoon enforces on the serialized
// genesis, 70 MiB.
//
// The real limit is the 75 MiB gRPC receive limit on the ABCI server in
// celestia-core (abci/server/grpc_server.go). CometBFT hands the genesis
// app_state to InitChain verbatim, so file size is wire size, and any
// multiplexer node replaying this chain from genesis runs InitChain against an
// embedded binary over gRPC. Exceeding it means those nodes can never sync from
// genesis, so we keep 5 MiB of margin.
const DefaultMaxSizeBytes = 70 << 20

// WarnSizeBytes is the size above which the report flags how little headroom is
// left.
const WarnSizeBytes = 65 << 20

// Options configures a spoon.
type Options struct {
	// ChainID of the new chain.
	ChainID string
	// GenesisTime the new chain starts at.
	GenesisTime time.Time
	// InitialHeight of the new chain, normally 1.
	InitialHeight int64
	// AppVersion written to consensus params. The export drops it, so it has to
	// be set explicitly.
	AppVersion uint64
	// Reserves are new accounts funded out of thin air.
	Reserves []Reserve
	// KeepPubKeys retains account public keys. They cost ~13 MiB on a
	// mocha-sized chain and are re-learned from each account's first signed tx,
	// so they are dropped by default.
	KeepPubKeys bool
	// NoPrune retains accounts that hold no funds.
	NoPrune bool
	// MaxSizeBytes fails the spoon if the serialized genesis exceeds it.
	// Zero means DefaultMaxSizeBytes.
	MaxSizeBytes int
	// ValidateGenesis, if set, is handed the assembled app state so that every
	// module's own ValidateGenesis runs against what the spoon produced. Without
	// it, state that is reachable on a live chain but illegal in a genesis file
	// only surfaces later, as an opaque failure inside `genesis gentx` or
	// `collect-gentxs`. The command wires in the app's module manager; this is a
	// hook so the package stays independent of the app.
	ValidateGenesis func(appState map[string]json.RawMessage) error
}

// Reserve is a new account created with a starting balance.
type Reserve struct {
	Address string `json:"address"`
	Amount  string `json:"amount"`
}

// Fork is a parsed `export --for-zero-height` document.
type Fork struct {
	ChainID         string
	InitialHeight   int64
	ConsensusParams *cmttypes.ConsensusParams
	AppState        map[string]json.RawMessage
}

// Result is a completed spoon.
type Result struct {
	Genesis *cmttypes.GenesisDoc
	// Bytes is the compact serialization of Genesis, and is what gets written
	// and hashed. collect-gentxs and convert-genesis both re-indent the file, so
	// it has to be re-compacted before publishing.
	Bytes  []byte
	Report *Report
}

// Transform spoons fork into a new genesis.
//
// defaultGenesis is the current binary's default genesis, which supplies the
// modules that are deliberately wiped. cdc must be the app codec so that every
// account type in the export can be unpacked.
func Transform(cdc codec.Codec, defaultGenesis map[string]json.RawMessage, fork *Fork, opts Options) (*Result, error) {
	if opts.MaxSizeBytes == 0 {
		opts.MaxSizeBytes = DefaultMaxSizeBytes
	}
	if opts.ChainID == "" {
		return nil, fmt.Errorf("chain id is required")
	}
	if opts.InitialHeight < 1 {
		return nil, fmt.Errorf("initial height must be at least 1, got %d", opts.InitialHeight)
	}
	if opts.AppVersion == 0 {
		return nil, fmt.Errorf("app version is required")
	}

	s := &spoon{cdc: cdc, fork: fork, opts: opts, report: &Report{ChainID: opts.ChainID}}

	if err := s.requireForZeroHeightExport(); err != nil {
		return nil, err
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	if err := s.creditStake(); err != nil {
		return nil, err
	}
	if err := s.reconcileStakingPools(); err != nil {
		return nil, err
	}
	if err := s.vaporize(); err != nil {
		return nil, err
	}
	s.zeroVestingDelegations()
	s.flattenVestedAccounts()
	s.prune()
	s.adoptOrphanBalances()
	if err := s.injectReserves(); err != nil {
		return nil, err
	}
	s.normalizeAccounts()

	genesis, err := s.assemble(defaultGenesis)
	if err != nil {
		return nil, err
	}

	bz, err := cmtjson.Marshal(genesis)
	if err != nil {
		return nil, fmt.Errorf("marshaling genesis: %w", err)
	}
	s.report.SizeBytes = len(bz)
	s.report.MaxSizeBytes = opts.MaxSizeBytes

	result := &Result{Genesis: genesis, Bytes: bz, Report: s.report}
	if err := Verify(cdc, result, opts); err != nil {
		return nil, err
	}
	return result, nil
}

// spoon carries the intermediate state of a single transform.
type spoon struct {
	cdc  codec.Codec
	fork *Fork
	opts Options

	auth         authtypes.GenesisState
	bank         banktypes.GenesisState
	staking      stakingtypes.GenesisState
	distribution distributiontypes.GenesisState
	slashing     slashingtypes.GenesisState
	gov          govv1.GenesisState
	minfee       minfeetypes.GenesisState
	transfer     ibctransfertypes.GenesisState
	ica          icagenesistypes.GenesisState
	channels     []channel

	// accounts is the working account list, in the export's order.
	accounts []sdk.AccountI
	// balances maps address to coins. sdk.Coins keeps denoms sorted and
	// deduplicated for us.
	balances map[string]sdk.Coins

	report *Report
}

// requireForZeroHeightExport rejects a plain `export`.
//
// A plain export leaves starting_info.stake at whatever it was when the
// delegation was last touched, which is stale for any delegation whose
// validator was later slashed. Crediting from it silently over-pays: on
// mocha-4 at height 13,125,871 it overshot the staking pools by 2.01M TIA
// across 1,275 of 3,746 delegations. The reconciliation in
// reconcileStakingPools would catch it, but failing here says why.
func (s *spoon) requireForZeroHeightExport() error {
	if s.fork.InitialHeight != 0 {
		return fmt.Errorf(
			"expected a --for-zero-height export (initial_height 0), got initial_height %d; "+
				"re-export with `celestia-appd export --for-zero-height`",
			s.fork.InitialHeight,
		)
	}

	var distribution distributiontypes.GenesisState
	if err := s.unmarshal(distributiontypes.ModuleName, &distribution); err != nil {
		return err
	}
	// prepForZeroHeightGenesis deletes slash events and nothing re-creates
	// them. Historical rewards are not a usable marker: they are deleted and
	// then rewritten by IncrementValidatorPeriod during re-initialization.
	if len(distribution.ValidatorSlashEvents) != 0 {
		return fmt.Errorf(
			"expected a --for-zero-height export (no validator slash events), got %d; "+
				"re-export with `celestia-appd export --for-zero-height`",
			len(distribution.ValidatorSlashEvents),
		)
	}
	for _, info := range distribution.DelegatorStartingInfos {
		if info.StartingInfo.Height != 0 {
			return fmt.Errorf(
				"expected a --for-zero-height export (starting infos re-initialized at height 0), "+
					"but %s/%s is at height %d",
				info.ValidatorAddress, info.DelegatorAddress, info.StartingInfo.Height,
			)
		}
	}
	return nil
}

func (s *spoon) load() error {
	modules := []struct {
		name string
		into proto.Message
	}{
		{authtypes.ModuleName, &s.auth},
		{banktypes.ModuleName, &s.bank},
		{stakingtypes.ModuleName, &s.staking},
		{distributiontypes.ModuleName, &s.distribution},
		{slashingtypes.ModuleName, &s.slashing},
		{govtypes.ModuleName, &s.gov},
		{minfeetypes.ModuleName, &s.minfee},
		{ibctransfertypes.ModuleName, &s.transfer},
		{icatypes.ModuleName, &s.ica},
	}
	for _, module := range modules {
		if err := s.unmarshal(module.name, module.into); err != nil {
			return err
		}
	}
	if err := s.loadChannels(); err != nil {
		return err
	}

	accounts, err := authtypes.UnpackAccounts(s.auth.Accounts)
	if err != nil {
		return fmt.Errorf("unpacking accounts: %w", err)
	}
	s.accounts = make([]sdk.AccountI, 0, len(accounts))
	for _, account := range accounts {
		s.accounts = append(s.accounts, account)
	}

	s.balances = make(map[string]sdk.Coins, len(s.bank.Balances))
	for _, balance := range s.bank.Balances {
		if _, ok := s.balances[balance.Address]; ok {
			return fmt.Errorf("duplicate balance for %s in export", balance.Address)
		}
		s.balances[balance.Address] = balance.Coins
	}

	s.report.In = Totals{
		Accounts: len(s.accounts),
		Balances: len(s.balances),
		Supply:   s.bank.Supply,
	}
	return nil
}

// credit adds coins to an address, creating the entry if needed.
func (s *spoon) credit(address string, amount math.Int) {
	if !amount.IsPositive() {
		return
	}
	coin := sdk.NewCoin(s.staking.Params.BondDenom, amount)
	s.balances[address] = s.balances[address].Add(coin)
}

func (s *spoon) unmarshal(module string, into proto.Message) error {
	raw, ok := s.fork.AppState[module]
	if !ok {
		return fmt.Errorf("export is missing the %q module", module)
	}
	if err := s.cdc.UnmarshalJSON(raw, into); err != nil {
		return fmt.Errorf("unmarshaling %q genesis: %w", module, err)
	}
	return nil
}

// channel is a port/channel pair from the ibc core genesis.
type channel struct {
	PortID    string `json:"port_id"`
	ChannelID string `json:"channel_id"`
}

// loadChannels pulls just the port/channel pairs out of the ibc core genesis,
// which is all that is needed to derive the transfer escrow addresses.
//
// Only these two fields are decoded on purpose. The ibc core genesis is a plain
// struct wrapping the client, connection and channel genesis states, and the
// client states are protobuf Any values that neither encoding/json nor the app
// codec will round-trip through that outer struct.
func (s *spoon) loadChannels() error {
	raw, ok := s.fork.AppState[ibcModuleName]
	if !ok {
		return fmt.Errorf("export is missing the %q module", ibcModuleName)
	}
	var core struct {
		ChannelGenesis struct {
			Channels []channel `json:"channels"`
		} `json:"channel_genesis"`
	}
	if err := json.Unmarshal(raw, &core); err != nil {
		return fmt.Errorf("reading ibc channels: %w", err)
	}
	s.channels = core.ChannelGenesis.Channels
	return nil
}

// LoadFork reads an exported genesis. Both the SDK's AppGenesis layout
// (consensus.params) and CometBFT's GenesisDoc layout (consensus_params) are
// accepted, since `export` emits the former and nodes run the latter.
//
// The envelope and the consensus params have to be decoded with different
// codecs, because the two layouts disagree on how 64-bit integers are written
// and the disagreement is not uniform within a single file. `export` marshals
// the whole AppGenesis with encoding/json, so initial_height is a bare number,
// but its consensus block goes through ConsensusGenesis.MarshalJSON, which uses
// cmtjson, so the params inside it are quoted strings. A CometBFT genesis.json
// is cmtjson all the way down. Decoding either layout with a single codec fails
// on half the file.
func LoadFork(raw []byte) (*Fork, error) {
	var doc struct {
		ChainID         string                     `json:"chain_id"`
		InitialHeight   json.RawMessage            `json:"initial_height"`
		ConsensusParams json.RawMessage            `json:"consensus_params"`
		Consensus       json.RawMessage            `json:"consensus"`
		AppState        map[string]json.RawMessage `json:"app_state"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing exported genesis: %w", err)
	}
	if len(doc.AppState) == 0 {
		return nil, fmt.Errorf("exported genesis has no app_state")
	}

	initialHeight, err := parseInitialHeight(doc.InitialHeight)
	if err != nil {
		return nil, err
	}

	fork := &Fork{ChainID: doc.ChainID, InitialHeight: initialHeight, AppState: doc.AppState}
	// The AppGenesis layout wins: `export` overwrites consensus with the live
	// params, while a stale top-level consensus_params may be left over from
	// the node's own genesis file.
	if len(doc.Consensus) != 0 {
		var consensus consensusGenesis
		if err := cmtjson.Unmarshal(doc.Consensus, &consensus); err != nil {
			return nil, fmt.Errorf("parsing consensus genesis: %w", err)
		}
		fork.ConsensusParams = consensus.Params
	}
	if fork.ConsensusParams == nil && len(doc.ConsensusParams) != 0 {
		if err := cmtjson.Unmarshal(doc.ConsensusParams, &fork.ConsensusParams); err != nil {
			return nil, fmt.Errorf("parsing consensus params: %w", err)
		}
	}
	if fork.ConsensusParams == nil {
		return nil, fmt.Errorf("exported genesis has no consensus params")
	}
	return fork, nil
}

type consensusGenesis struct {
	Params *cmttypes.ConsensusParams `json:"params"`
}

// parseInitialHeight reads initial_height from either layout, where it is a bare
// number or a quoted one depending on which codec wrote the envelope.
func parseInitialHeight(raw json.RawMessage) (int64, error) {
	text := strings.Trim(string(raw), `"`)
	if text == "" || text == "null" {
		return 0, nil
	}
	height, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing initial_height %s: %w", raw, err)
	}
	return height, nil
}

// ibcModuleName is the ibc core module's genesis key.
const ibcModuleName = "ibc"
