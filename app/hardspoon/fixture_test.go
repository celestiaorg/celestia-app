package hardspoon_test

import (
	"encoding/json"
	"maps"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/app/hardspoon"
	"github.com/celestiaorg/celestia-app/v9/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v9/test/util/testnode"
	minfeetypes "github.com/celestiaorg/celestia-app/v9/x/minfee/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	icagenesistypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/genesis/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"github.com/stretchr/testify/require"
)

const denom = "utia"

// testApp builds an app purely to borrow its codec and default genesis.
func testApp(t *testing.T) *app.App {
	t.Helper()
	return testAppWithChainID(t, "")
}

// testAppWithChainID builds an app that will accept InitChain for chainID.
// baseapp rejects an InitChain whose chain id does not match the one it was
// constructed with.
func testAppWithChainID(t *testing.T, chainID string) *app.App {
	t.Helper()

	options := []func(*baseapp.BaseApp){}
	if chainID != "" {
		options = append(options, baseapp.SetChainID(chainID))
	}
	return app.New(
		log.NewNopLogger(), dbm.NewMemDB(), nil, 0, 0,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
		options...,
	)
}

// fixture is a hand-built export that behaves like a --for-zero-height snapshot.
//
// The numbers are chosen so the staking pools reconcile exactly:
//
//	delegations   10000 + 20000 = 30000 = bonded pool
//	unbonding       100 +   200 =   300 = not-bonded pool
//
// and so that every account category the transform has to handle is present.
type fixture struct {
	// Named addresses, for assertions.
	Delegator1 string
	Delegator2 string
	Empty      string
	Vesting    string
	ICA        string
	Escrow     string
	Orphan     string

	Accounts      []authtypes.GenesisAccount
	Balances      []banktypes.Balance
	DenomMetadata []banktypes.Metadata
	Staking       stakingtypes.GenesisState
	Distribution  distributiontypes.GenesisState
	Transfer      ibctransfertypes.GenesisState
	MinFee        minfeetypes.GenesisState
	Gov           govv1.GenesisState
	Warp          warptypes.GenesisState
	Channels      []channelFixture
}

type channelFixture struct {
	PortID    string `json:"port_id"`
	ChannelID string `json:"channel_id"`
}

func address(seed string) string {
	return sdk.AccAddress(secp256k1.GenPrivKeyFromSecret([]byte(seed)).PubKey().Address()).String()
}

func newFixture() *fixture {
	f := &fixture{
		Delegator1: address("delegator-one"),
		Delegator2: address("delegator-two"),
		Empty:      address("holds-nothing"),
		Vesting:    address("vesting-account"),
		ICA:        address("interchain-account"),
		Orphan:     address("balance-without-account"),
	}
	// The escrow address is derived, exactly as the transform derives it.
	f.Escrow = ibctransfertypes.GetEscrowAddress(ibctransfertypes.PortID, "channel-0").String()
	f.Channels = []channelFixture{
		{PortID: ibctransfertypes.PortID, ChannelID: "channel-0"},
		// A non-transfer port must be ignored.
		{PortID: "icahost", ChannelID: "channel-1"},
	}

	validator := sdk.ValAddress(secp256k1.GenPrivKeyFromSecret([]byte("validator")).PubKey().Address()).String()

	// The reconciliation derives what each pool should hold from the validators,
	// so the delegations below have to belong to one. Bonded, holding exactly the
	// 30000 of delegation principal; the 300 of unbonding delegations sits in the
	// not-bonded pool instead.
	bondedValidator, err := stakingtypes.NewValidator(
		validator,
		ed25519.GenPrivKeyFromSecret([]byte("validator-consensus")).PubKey(),
		stakingtypes.Description{Moniker: "fixture"},
	)
	if err != nil {
		panic(err)
	}
	bondedValidator.Status = stakingtypes.Bonded
	bondedValidator.Tokens = math.NewInt(30_000)
	bondedValidator.DelegatorShares = math.LegacyNewDec(30_000)

	bonded := authtypes.NewEmptyModuleAccount(stakingtypes.BondedPoolName, authtypes.Burner, authtypes.Staking)
	notBonded := authtypes.NewEmptyModuleAccount(stakingtypes.NotBondedPoolName, authtypes.Burner, authtypes.Staking)
	distribution := authtypes.NewEmptyModuleAccount(distributiontypes.ModuleName)
	feeCollector := authtypes.NewEmptyModuleAccount(authtypes.FeeCollectorName)

	// delegator1 has signed before: it carries a public key and a live sequence.
	delegator1Key := secp256k1.GenPrivKeyFromSecret([]byte("delegator-one")).PubKey()
	delegator1 := authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(f.Delegator1), delegator1Key, 10, 5)
	// delegator2 has never signed: no public key, and no balance until its stake
	// comes back.
	delegator2 := authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(f.Delegator2), nil, 11, 0)
	empty := authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(f.Empty), nil, 12, 0)

	vestingBase := authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(f.Vesting), nil, 13, 0)
	vesting, err := vestingtypes.NewContinuousVestingAccount(
		vestingBase,
		sdk.NewCoins(sdk.NewInt64Coin(denom, 5_000)),
		time.Unix(1_600_000_000, 0).Unix(),
		time.Unix(1_900_000_000, 0).Unix(),
	)
	if err != nil {
		panic(err)
	}
	// Stake used to be tracked against the vesting schedule.
	vesting.DelegatedVesting = sdk.NewCoins(sdk.NewInt64Coin(denom, 4_000))
	vesting.DelegatedFree = sdk.NewCoins(sdk.NewInt64Coin(denom, 1_000))

	ica := icatypes.NewInterchainAccount(
		authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(f.ICA), nil, 14, 0),
		"controller-owner",
	)
	escrow := authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(f.Escrow), nil, 15, 0)

	f.Accounts = []authtypes.GenesisAccount{
		bonded, notBonded, distribution, feeCollector,
		delegator1, delegator2, empty, vesting, ica, escrow,
	}

	coins := func(amount int64) sdk.Coins { return sdk.NewCoins(sdk.NewInt64Coin(denom, amount)) }
	f.Balances = []banktypes.Balance{
		{Address: bonded.GetAddress().String(), Coins: coins(30_000)},
		{Address: notBonded.GetAddress().String(), Coins: coins(300)},
		{Address: distribution.GetAddress().String(), Coins: coins(5_000)},
		{Address: feeCollector.GetAddress().String(), Coins: coins(7)},
		{Address: f.Delegator1, Coins: coins(1_000)},
		{Address: f.ICA, Coins: coins(500)},
		{Address: f.Escrow, Coins: coins(700)},
		{Address: f.Orphan, Coins: coins(300)},
	}

	// Mirrors mocha-4's utia entry. It has to be valid, because hardspoon drops
	// metadata that bank's own Metadata.Validate rejects.
	f.DenomMetadata = []banktypes.Metadata{{
		Base:    denom,
		Display: "TIA",
		Symbol:  "TIA",
		Name:    "TIA",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: denom, Exponent: 0},
			{Denom: "TIA", Exponent: 6},
		},
	}}

	f.Staking = stakingtypes.GenesisState{
		Params:         stakingtypes.DefaultParams(),
		LastTotalPower: math.NewInt(30),
		Exported:       true,
		Validators:     []stakingtypes.Validator{bondedValidator},
		Delegations: []stakingtypes.Delegation{
			{DelegatorAddress: f.Delegator1, ValidatorAddress: validator, Shares: math.LegacyNewDec(10_000)},
			{DelegatorAddress: f.Delegator2, ValidatorAddress: validator, Shares: math.LegacyNewDec(20_000)},
		},
		UnbondingDelegations: []stakingtypes.UnbondingDelegation{{
			DelegatorAddress: f.Delegator1,
			ValidatorAddress: validator,
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{Balance: math.NewInt(100), InitialBalance: math.NewInt(100), UnbondingId: 1},
				{Balance: math.NewInt(200), InitialBalance: math.NewInt(200), UnbondingId: 2},
			},
		}},
	}
	f.Staking.Params.BondDenom = denom

	f.Distribution = distributiontypes.GenesisState{
		Params:  distributiontypes.DefaultParams(),
		FeePool: distributiontypes.FeePool{CommunityPool: sdk.NewDecCoins(sdk.NewInt64DecCoin(denom, 5_000))},
		DelegatorStartingInfos: []distributiontypes.DelegatorStartingInfoRecord{
			{
				DelegatorAddress: f.Delegator1,
				ValidatorAddress: validator,
				StartingInfo:     distributiontypes.NewDelegatorStartingInfo(1, math.LegacyNewDec(10_000), 0),
			},
			{
				DelegatorAddress: f.Delegator2,
				ValidatorAddress: validator,
				StartingInfo:     distributiontypes.NewDelegatorStartingInfo(1, math.LegacyNewDec(20_000), 0),
			},
		},
	}

	f.Transfer = ibctransfertypes.GenesisState{
		PortId:        ibctransfertypes.PortID,
		Params:        ibctransfertypes.DefaultParams(),
		DenomTraces:   ibctransfertypes.Traces{{Path: "transfer/channel-7", BaseDenom: "stake"}},
		TotalEscrowed: sdk.NewCoins(sdk.NewInt64Coin(denom, 700)),
	}

	f.MinFee = minfeetypes.GenesisState{
		// The export fills only this field; params is left at the default.
		NetworkMinGasPrice: math.LegacyNewDecWithPrec(5, 3),
		Params:             minfeetypes.Params{NetworkMinGasPrice: minfeetypes.DefaultNetworkMinGasPrice},
	}

	f.Gov = govv1.GenesisState{
		StartingProposalId: 17,
		Params:             func() *govv1.Params { p := govv1.DefaultParams(); return &p }(),
		Proposals:          []*govv1.Proposal{{Id: 3}},
	}

	return f
}

// addVesting appends a funded ContinuousVestingAccount with the given schedule
// and returns its address. startTime after endTime is allowed on purpose: that is
// what two of mocha-4's accounts look like.
func (f *fixture) addVesting(t *testing.T, seed string, startTime, endTime int64, amount int64) string {
	t.Helper()

	address := address(seed)
	base := authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(address), nil, uint64(len(f.Accounts)), 0)
	// Assembled directly rather than through NewContinuousVestingAccount, which
	// rejects start >= end. That is the whole reason the malformed accounts exist
	// on mocha-4: MsgCreateVestingAccount takes the same path this does and only
	// checks end_time > 0.
	account := vestingtypes.NewContinuousVestingAccountRaw(
		&vestingtypes.BaseVestingAccount{
			BaseAccount:     base,
			OriginalVesting: sdk.NewCoins(sdk.NewInt64Coin(denom, amount)),
			EndTime:         endTime,
		},
		startTime,
	)

	f.Accounts = append(f.Accounts, account)
	f.Balances = append(f.Balances, banktypes.Balance{
		Address: address, Coins: sdk.NewCoins(sdk.NewInt64Coin(denom, amount)),
	})
	return address
}

// addPermanentLocked appends a funded PermanentLockedAccount, which has no end
// time and never vests, and returns its address.
func (f *fixture) addPermanentLocked(t *testing.T, seed string, amount int64) string {
	t.Helper()

	address := address(seed)
	base := authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(address), nil, uint64(len(f.Accounts)), 0)
	account, err := vestingtypes.NewPermanentLockedAccount(base, sdk.NewCoins(sdk.NewInt64Coin(denom, amount)))
	require.NoError(t, err)

	f.Accounts = append(f.Accounts, account)
	f.Balances = append(f.Balances, banktypes.Balance{
		Address: address, Coins: sdk.NewCoins(sdk.NewInt64Coin(denom, amount)),
	})
	return address
}

// addAccount appends a funded base account and returns its address.
func (f *fixture) addAccount(seed string, coins sdk.Coins) string {
	address := address(seed)
	base := authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(address), nil, uint64(len(f.Accounts)), 0)
	f.Accounts = append(f.Accounts, base)
	f.Balances = append(f.Balances, banktypes.Balance{Address: address, Coins: coins})
	return address
}

// addWarpToken registers a warp token under the given 32-byte hex id and
// returns the denom its coins carry in the bank. A synthetic token mints under
// "hyperlane/"+id, exactly as the warp msg server derives it; a collateral
// token escrows the chain's own denom.
func (f *fixture) addWarpToken(t *testing.T, tokenType warptypes.HypTokenType, id string) string {
	t.Helper()

	tokenID, err := util.DecodeHexAddress(id)
	require.NoError(t, err)

	token := warptypes.HypToken{
		Id:                tokenID,
		TokenType:         tokenType,
		OriginDenom:       denom,
		CollateralBalance: math.ZeroInt(),
	}
	if tokenType == warptypes.HYP_TOKEN_TYPE_SYNTHETIC {
		token.OriginDenom = "hyperlane/" + tokenID.String()
	}
	f.Warp.Tokens = append(f.Warp.Tokens, token)
	return token.OriginDenom
}

// withOperator adds a funded account whose key lives in the returned keyring, so
// that a gentx can later be signed by an account the export carried over.
//
// The operator needs enough to cover its self-delegation plus the gentx fee: a
// gentx runs through the full ante handler at InitChain, including the network
// minimum gas price.
func (f *fixture) withOperator(t *testing.T, name string, balance int64) keyring.Keyring {
	t.Helper()

	kr, balances, accounts := testnode.FundKeyringAccounts(name)
	require.Len(t, accounts, 1)

	// Renumber so it sits after the fixture's own accounts.
	require.NoError(t, accounts[0].SetAccountNumber(uint64(len(f.Accounts))))
	f.Accounts = append(f.Accounts, accounts[0])
	f.Balances = append(f.Balances, banktypes.Balance{
		Address: balances[0].Address,
		Coins:   sdk.NewCoins(sdk.NewInt64Coin(denom, balance)),
	})
	return kr
}

// gentx builds a signed MsgCreateValidator for an account in kr and returns it
// as the genutil genesis state, ready to splice into an app state.
//
// gasPrice drives the fee, which matters: a gentx is delivered through the full
// ante handler at InitChain, so it has to cover the network minimum gas price.
func gentx(t *testing.T, cdc codec.Codec, kr keyring.Keyring, name, chainID string, stake int64, gasPrice float64) json.RawMessage {
	t.Helper()

	validator := genesis.NewDefaultValidator(name)
	validator.Stake = stake

	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	tx, err := validator.GenTx(encodingConfig, kr, chainID, gasPrice)
	require.NoError(t, err)

	encoded, err := encodingConfig.TxConfig.TxJSONEncoder()(tx)
	require.NoError(t, err)

	// The codec has to do this: the proto field is gen_txs, while the Go struct
	// tag is gentxs, and genutil reads it back through the codec.
	state, err := cdc.MarshalJSON(&genutiltypes.GenesisState{GenTxs: []json.RawMessage{encoded}})
	require.NoError(t, err)
	return state
}

// supply sums the fixture's balances, which is what a real export would state.
func (f *fixture) supply() sdk.Coins {
	total := sdk.NewCoins()
	for _, balance := range f.Balances {
		total = total.Add(balance.Coins...)
	}
	return total
}

// fork renders the fixture as a Fork, starting from the app's default genesis so
// that every module the transform reads is present.
func (f *fixture) fork(t *testing.T, capp *app.App) *hardspoon.Fork {
	t.Helper()
	cdc := capp.AppCodec()

	appState := map[string]json.RawMessage{}
	maps.Copy(appState, capp.DefaultGenesis())

	packed, err := authtypes.PackAccounts(f.Accounts)
	require.NoError(t, err)

	set := func(module string, state proto.Message) {
		raw, err := cdc.MarshalJSON(state)
		require.NoError(t, err)
		appState[module] = raw
	}

	set(authtypes.ModuleName, &authtypes.GenesisState{Params: authtypes.DefaultParams(), Accounts: packed})
	set(banktypes.ModuleName, &banktypes.GenesisState{
		Params:        banktypes.DefaultParams(),
		Balances:      f.Balances,
		Supply:        f.supply(),
		DenomMetadata: f.DenomMetadata,
	})
	set(stakingtypes.ModuleName, &f.Staking)
	set(distributiontypes.ModuleName, &f.Distribution)
	set(slashingtypes.ModuleName, &slashingtypes.GenesisState{
		Params: slashingtypes.DefaultParams(),
		SigningInfos: []slashingtypes.SigningInfo{{
			Address:              "celestiavalcons1abc",
			ValidatorSigningInfo: slashingtypes.ValidatorSigningInfo{Address: "celestiavalcons1abc"},
		}},
	})
	set(govtypes.ModuleName, &f.Gov)
	set(minfeetypes.ModuleName, &f.MinFee)
	set(ibctransfertypes.ModuleName, &f.Transfer)
	set(icatypes.ModuleName, icagenesistypes.DefaultGenesis())
	set(warptypes.ModuleName, &f.Warp)

	channels, err := json.Marshal(map[string]any{
		"channel_genesis": map[string]any{"channels": f.Channels},
	})
	require.NoError(t, err)
	appState["ibc"] = channels

	return &hardspoon.Fork{
		ChainID:       "mocha-4",
		InitialHeight: 0,
		ConsensusParams: &cmttypes.ConsensusParams{
			Block:     cmttypes.BlockParams{MaxBytes: 33_554_432, MaxGas: -1},
			Evidence:  cmttypes.EvidenceParams{MaxAgeNumBlocks: 559_940, MaxAgeDuration: time.Hour, MaxBytes: 1_048_576},
			Validator: cmttypes.ValidatorParams{PubKeyTypes: []string{cmttypes.ABCIPubKeyTypeEd25519}},
			// The export drops these two; hardspoon has to write them.
			Version: cmttypes.VersionParams{App: 0},
		},
		AppState: appState,
	}
}

func defaultOptions() hardspoon.Options {
	return hardspoon.Options{
		ChainID:       "mocha-5",
		GenesisTime:   time.Date(2026, 9, 1, 14, 0, 0, 0, time.UTC),
		InitialHeight: 1,
		AppVersion:    9,
	}
}

// appState decodes a module's genesis out of a result.
func appStateOf(t *testing.T, cdc codec.Codec, result *hardspoon.Result, module string, into proto.Message) {
	t.Helper()
	var modules map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result.Genesis.AppState, &modules))
	require.Contains(t, modules, module)
	require.NoError(t, cdc.UnmarshalJSON(modules[module], into))
}
