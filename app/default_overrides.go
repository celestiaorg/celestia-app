package app

import (
	"encoding/json"
	"time"

	"cosmossdk.io/math"
	"cosmossdk.io/x/circuit"
	circuittypes "cosmossdk.io/x/circuit/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v6/app/params"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/x/mint"
	minttypes "github.com/celestiaorg/celestia-app/v6/x/mint/types"
	tmcfg "github.com/cometbft/cometbft/config"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/codec"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/bank"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward"
	packetforwardtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	ica "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts"
	icagenesistypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/genesis/types"
	ibc "github.com/cosmos/ibc-go/v8/modules/core"
	ibcclienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	ibctypes "github.com/cosmos/ibc-go/v8/modules/core/types"
)

const (
	mebibyte = 1048576
)

var (
	_ module.HasGenesisBasics = bankModule{}
	_ module.HasGenesisBasics = circuitModule{}
	_ module.HasGenesisBasics = govModule{}
	_ module.HasGenesisBasics = ibcModule{}
	_ module.HasGenesisBasics = icaModule{}
	_ module.HasGenesisBasics = mintModule{}
	_ module.HasGenesisBasics = slashingModule{}
	_ module.HasGenesisBasics = stakingModule{}
)

// bankModule defines a custom wrapper around the x/bank module's AppModuleBasic
// implementation to provide custom default genesis state.
type bankModule struct {
	bank.AppModule
}

// DefaultGenesis returns custom x/bank module genesis state.
func (bankModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	metadata := banktypes.Metadata{
		Description: "The native token of the Celestia network.",
		Base:        params.BondDenom,
		Name:        params.DisplayDenom,
		Display:     params.DisplayDenom,
		Symbol:      params.DisplayDenom,
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    params.BondDenom,
				Exponent: 0,
				Aliases: []string{
					params.BondDenomAlias,
				},
			},
			{
				Denom:    params.DisplayDenom,
				Exponent: 6,
				Aliases:  []string{},
			},
		},
	}

	genState := banktypes.DefaultGenesisState()
	genState.DenomMetadata = append(genState.DenomMetadata, metadata)

	return cdc.MustMarshalJSON(genState)
}

// stakingModule wraps the x/staking module in order to overwrite specific
// ModuleManager APIs.
type stakingModule struct {
	staking.AppModule
}

// DefaultGenesis returns custom x/staking module genesis state.
func (stakingModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	genesis := stakingtypes.DefaultGenesisState()
	genesis.Params.UnbondingTime = appconsts.UnbondingTime
	genesis.Params.BondDenom = params.BondDenom
	genesis.Params.MinCommissionRate = math.LegacyNewDecWithPrec(5, 2) // 5%

	return cdc.MustMarshalJSON(genesis)
}

// slashingModule wraps the x/slashing module in order to overwrite specific
// ModuleManager APIs.
type slashingModule struct {
	slashing.AppModule
}

// DefaultGenesis returns custom x/staking module genesis state.
func (slashingModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	genesis := slashingtypes.DefaultGenesisState()
	genesis.Params.MinSignedPerWindow = math.LegacyNewDecWithPrec(75, 2) // 75%
	genesis.Params.SignedBlocksWindow = 5000
	genesis.Params.DowntimeJailDuration = time.Minute * 1
	genesis.Params.SlashFractionDoubleSign = math.LegacyNewDecWithPrec(2, 2) // 2%
	genesis.Params.SlashFractionDowntime = math.LegacyZeroDec()              // 0%

	return cdc.MustMarshalJSON(genesis)
}

type ibcModule struct {
	ibc.AppModule
}

// DefaultGenesis returns custom x/ibc module genesis state.
func (ibcModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	// per ibc documentation, this value should be 3-5 times the expected block
	// time. The expected block time is 15 seconds, therefore this value is 75
	// seconds.
	maxBlockTime := appconsts.GoalBlockTime * 5
	gs := ibctypes.DefaultGenesisState()
	gs.ClientGenesis.Params.AllowedClients = []string{"06-solomachine", "07-tendermint"}
	gs.ConnectionGenesis.Params.MaxExpectedTimePerBlock = uint64(maxBlockTime.Nanoseconds())

	return cdc.MustMarshalJSON(gs)
}

// icaModule defines a custom wrapper around the ica module to provide custom
// default genesis state.
type icaModule struct {
	ica.AppModule
}

// DefaultGenesis returns custom ica module genesis state.
func (icaModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := icagenesistypes.DefaultGenesis()
	gs.HostGenesisState.Params.AllowMessages = IcaAllowMessages()
	gs.HostGenesisState.Params.HostEnabled = true
	gs.ControllerGenesisState.Params.ControllerEnabled = false
	return cdc.MustMarshalJSON(gs)
}

// pfm is a wrapper around packetforward.AppModule which adds required no-op migrations for upgrade.
type pfm struct {
	packetforward.AppModule
}

// RegisterServices needs to be overridden to add a no-op handler when going from v1 -> v2
// the existing app module (v8) has a built-in migration for going from v2 -> v3
func (am pfm) RegisterServices(cfg module.Configurator) {
	err := cfg.RegisterMigration(packetforwardtypes.ModuleName, 1, func(sdk.Context) error {
		// a no-op registration needs to happen from v1 -> v2.
		return nil
	})
	if err != nil {
		panic(err)
	}
	// handle existing migrations from v2 -> v3
	am.AppModule.RegisterServices(cfg)
}

type mintModule struct {
	mint.AppModule
}

// DefaultGenesis returns custom x/mint module genesis state.
func (mintModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	genState := minttypes.DefaultGenesisState()
	genState.BondDenom = params.BondDenom

	return cdc.MustMarshalJSON(genState)
}

// govModule is a custom wrapper around the x/gov module's AppModuleBasic
// implementation to provide custom default genesis state.
type govModule struct {
	gov.AppModule
}

// DefaultGenesis returns custom x/gov module genesis state.
func (govModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	genState := govtypes.DefaultGenesisState()
	day := time.Hour * 24
	oneWeek := day * 7
	tia := int64(1_000_000)                                                                     // 1 TIA = 1,000,000 utia
	minDeposit := sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(10_000*tia)))          // 10,000 TIA
	expeditedMinDeposit := sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(50_000*tia))) // 50,000 TIA

	genState.Params.MinDeposit = minDeposit
	genState.Params.MaxDepositPeriod = &oneWeek
	genState.Params.VotingPeriod = &oneWeek
	genState.Params.ExpeditedMinDeposit = expeditedMinDeposit

	return cdc.MustMarshalJSON(genState)
}

type circuitModule struct {
	circuit.AppModule
}

// DefaultGenesis returns custom x/circuit module genesis state.
func (circuitModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	genState := circuittypes.DefaultGenesisState()

	// block upgrade modules by default
	genState.DisabledTypeUrls = []string{
		sdk.MsgTypeURL(&upgradetypes.MsgSoftwareUpgrade{}),
		sdk.MsgTypeURL(&upgradetypes.MsgCancelUpgrade{}),
		sdk.MsgTypeURL(&ibcclienttypes.MsgIBCSoftwareUpgrade{}),
	}

	return cdc.MustMarshalJSON(genState)
}

// DefaultConsensusParams returns default consensus params.
func DefaultConsensusParams() *tmproto.ConsensusParams {
	return &tmproto.ConsensusParams{
		Block:    DefaultBlockParams(),
		Evidence: EvidenceParams(),
		Validator: &tmproto.ValidatorParams{
			PubKeyTypes: coretypes.DefaultValidatorParams().PubKeyTypes,
		},
		Version: &tmproto.VersionParams{
			App: appconsts.Version,
		},
	}
}

// DefaultBlockParams returns a default BlockParams with a MaxBytes determined
// using a goal square size.
func DefaultBlockParams() *tmproto.BlockParams {
	return &tmproto.BlockParams{
		MaxBytes: appconsts.DefaultMaxBytes,
		MaxGas:   -1,
	}
}

// EvidenceParams returns the evidence params defined in CIP-37. The evidence
// parameters are not modifiable by governance so a consensus breaking release
// is needed to modify the evidence parameters.
func EvidenceParams() *tmproto.EvidenceParams {
	return &tmproto.EvidenceParams{
		MaxAgeNumBlocks: appconsts.MaxAgeNumBlocks,
		MaxAgeDuration:  appconsts.MaxAgeDuration,
		MaxBytes:        coretypes.DefaultEvidenceParams().MaxBytes,
	}
}

func DefaultConsensusConfig() *tmcfg.Config {
	cfg := tmcfg.DefaultConfig()
	// Set broadcast timeout to be 50 seconds in order to avoid timeouts for long block times
	cfg.RPC.TimeoutBroadcastTxCommit = 50 * time.Second
	// this value should be the same as the largest possible response. In this case, that's
	// likely Unconfirmed txs for a full mempool and a few extra bytes.
	cfg.RPC.MaxBodyBytes = appconsts.MempoolSize + (mebibyte * 32)
	cfg.RPC.GRPCListenAddress = "tcp://127.0.0.1:9098"

	cfg.Mempool.TTLNumBlocks = 12
	cfg.Mempool.TTLDuration = 0 * time.Second
	cfg.Mempool.MaxTxBytes = appconsts.MaxTxSize
	cfg.Mempool.MaxTxsBytes = appconsts.MempoolSize
	cfg.Mempool.Type = tmcfg.MempoolTypeCAT
	cfg.Mempool.MaxGossipDelay = time.Second * 60

	cfg.Consensus.TimeoutPropose = appconsts.TimeoutPropose
	cfg.Consensus.TimeoutCommit = appconsts.TimeoutCommit
	cfg.Consensus.SkipTimeoutCommit = false

	cfg.TxIndex.Indexer = "null"
	cfg.Storage.DiscardABCIResponses = true

	cfg.P2P.SendRate = 24 * mebibyte
	cfg.P2P.RecvRate = 24 * mebibyte

	return cfg
}

func DefaultAppConfig() *serverconfig.Config {
	cfg := serverconfig.DefaultConfig()
	cfg.API.Enable = false
	cfg.GRPC.Enable = false
	cfg.GRPCWeb.Enable = false

	// the default snapshot interval was determined by picking a large enough
	// value as to not dramatically increase resource requirements while also
	// being greater than zero so that there are more nodes that will serve
	// snapshots to nodes that state sync
	cfg.StateSync.SnapshotInterval = 1500
	cfg.StateSync.SnapshotKeepRecent = 2
	// this is set to an empty string. As an empty string, the binary will use
	// the hardcoded default gas price. To override this, the user must set the
	// minimum gas prices in the app.toml file.
	cfg.MinGasPrices = ""
	cfg.GRPC.MaxRecvMsgSize = appconsts.DefaultUpperBoundMaxBytes * 2
	cfg.GRPC.MaxSendMsgSize = appconsts.DefaultUpperBoundMaxBytes * 2
	return cfg
}
