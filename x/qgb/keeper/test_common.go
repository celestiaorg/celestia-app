package keeper

// TODO uncomment after adding logic for other messages
// import (
//	"fmt"
//	"github.com/celestiaorg/celestia-app/x/qgb/types"
//	"github.com/cosmos/cosmos-sdk/baseapp"
//	"github.com/cosmos/cosmos-sdk/codec"
//	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
//	ccodec "github.com/cosmos/cosmos-sdk/crypto/codec"
//	"github.com/cosmos/cosmos-sdk/std"
//	"github.com/cosmos/cosmos-sdk/store"
//	sdk "github.com/cosmos/cosmos-sdk/types"
//	"github.com/cosmos/cosmos-sdk/types/module"
//	"github.com/cosmos/cosmos-sdk/x/auth"
//	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
//	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
//	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
//	"github.com/cosmos/cosmos-sdk/x/bank"
//	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
//	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
//	"github.com/cosmos/cosmos-sdk/x/capability"
//	"github.com/cosmos/cosmos-sdk/x/crisis"
//	"github.com/cosmos/cosmos-sdk/x/distribution"
//	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
//	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
//	"github.com/cosmos/cosmos-sdk/x/evidence"
//	"github.com/cosmos/cosmos-sdk/x/genutil"
//	"github.com/cosmos/cosmos-sdk/x/mint"
//	"github.com/cosmos/cosmos-sdk/x/params"
//	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
//	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
//	"github.com/cosmos/cosmos-sdk/x/slashing"
//	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
//	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
//	"github.com/cosmos/cosmos-sdk/x/staking"
//	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
//	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
//	"github.com/cosmos/cosmos-sdk/x/upgrade"
//	"github.com/stretchr/testify/require"
//	"github.com/tendermint/tendermint/libs/log"
//	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
//	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
//
//	// TODO add payment module
//	dbm "github.com/tendermint/tm-db"
//	"testing"
//	"time"
//)
//
//var (
//	// ModuleBasics is a mock module basic manager for testing
//	ModuleBasics = module.NewBasicManager(
//		auth.AppModuleBasic{},
//		genutil.AppModuleBasic{},
//		bank.AppModuleBasic{},
//		capability.AppModuleBasic{},
//		staking.AppModuleBasic{},
//		mint.AppModuleBasic{},
//		distribution.AppModuleBasic{},
//		params.AppModuleBasic{},
//		crisis.AppModuleBasic{},
//		slashing.AppModuleBasic{},
//		upgrade.AppModuleBasic{},
//		evidence.AppModuleBasic{},
//		vesting.AppModuleBasic{},
//	)
//	// TestingStakeParams is a set of staking params for testing
//	TestingStakeParams = stakingtypes.Params{
//		UnbondingTime:     100,
//		MaxValidators:     10,
//		MaxEntries:        10,
//		HistoricalEntries: 10000,
//		BondDenom:         "stake",
//	}
//)
//
//// TestInput stores the various keepers required to test gravity
//type TestInput struct {
//	QgbKeeper      Keeper
//	AccountKeeper  authkeeper.AccountKeeper
//	StakingKeeper  stakingkeeper.Keeper
//	SlashingKeeper slashingkeeper.Keeper
//	DistKeeper     distrkeeper.Keeper
//	BankKeeper     bankkeeper.BaseKeeper
//	Context        sdk.Context
//	Marshaler      codec.Codec
//	LegacyAmino    *codec.LegacyAmino
//}
//
//// CreateTestEnv creates the keeper testing environment for gravity
//func CreateTestEnv(t *testing.T) TestInput {
//	t.Helper()
//
//	// Initialize store keys
//	qgbKey := sdk.NewKVStoreKey(types.StoreKey)
//	keyAcc := sdk.NewKVStoreKey(authtypes.StoreKey)
//	keyStaking := sdk.NewKVStoreKey(stakingtypes.StoreKey)
//	keyBank := sdk.NewKVStoreKey(banktypes.StoreKey)
//	keyDistro := sdk.NewKVStoreKey(distrtypes.StoreKey)
//	keyParams := sdk.NewKVStoreKey(paramstypes.StoreKey)
//	tkeyParams := sdk.NewTransientStoreKey(paramstypes.TStoreKey)
//	keySlashing := sdk.NewKVStoreKey(slashingtypes.StoreKey)
//
//	// Initialize memory database and mount stores on it
//	db := dbm.NewMemDB()
//	ms := store.NewCommitMultiStore(db)
//	ms.MountStoreWithDB(qgbKey, sdk.StoreTypeIAVL, db)
//	ms.MountStoreWithDB(keyAcc, sdk.StoreTypeIAVL, db)
//	ms.MountStoreWithDB(keyParams, sdk.StoreTypeIAVL, db)
//	ms.MountStoreWithDB(keyStaking, sdk.StoreTypeIAVL, db)
//	ms.MountStoreWithDB(keyBank, sdk.StoreTypeIAVL, db)
//	ms.MountStoreWithDB(keyDistro, sdk.StoreTypeIAVL, db)
//	ms.MountStoreWithDB(tkeyParams, sdk.StoreTypeTransient, db)
//	ms.MountStoreWithDB(keySlashing, sdk.StoreTypeIAVL, db)
//	err := ms.LoadLatestVersion()
//	require.Nil(t, err)
//
//	// Create sdk.Context
//	ctx := sdk.NewContext(ms, tmproto.Header{
//		Version: tmversion.Consensus{
//			Block: 0,
//			App:   0,
//		},
//		ChainID: "",
//		Height:  1234567,
//		Time:    time.Date(2020, time.April, 22, 12, 0, 0, 0, time.UTC),
//		LastBlockId: tmproto.BlockID{
//			Hash: []byte{},
//			PartSetHeader: tmproto.PartSetHeader{
//				Total: 0,
//				Hash:  []byte{},
//			},
//		},
//		LastCommitHash:     []byte{},
//		DataHash:           []byte{},
//		ValidatorsHash:     []byte{},
//		NextValidatorsHash: []byte{},
//		ConsensusHash:      []byte{},
//		AppHash:            []byte{},
//		LastResultsHash:    []byte{},
//		EvidenceHash:       []byte{},
//		ProposerAddress:    []byte{},
//	}, false, log.TestingLogger())
//
//	cdc := MakeTestCodec()
//	marshaler := MakeTestMarshaler()
//
//	paramsKeeper := paramskeeper.NewKeeper(marshaler, cdc, keyParams, tkeyParams)
//	paramsKeeper.Subspace(authtypes.ModuleName)
//	paramsKeeper.Subspace(banktypes.ModuleName)
//	paramsKeeper.Subspace(stakingtypes.ModuleName)
//	paramsKeeper.Subspace(distrtypes.ModuleName)
//	paramsKeeper.Subspace(types.DefaultParamspace)
//	paramsKeeper.Subspace(slashingtypes.ModuleName)
//
//	// this is also used to initialize module accounts for all the map keys
//	maccPerms := map[string][]string{
//		authtypes.FeeCollectorName:     nil,
//		distrtypes.ModuleName:          nil,
//		stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
//		stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
//		types.ModuleName:               {authtypes.Minter, authtypes.Burner},
//	}
//
//	accountKeeper := authkeeper.NewAccountKeeper(
//		marshaler,
//		keyAcc, // target store
//		getSubspace(paramsKeeper, authtypes.ModuleName),
//		authtypes.ProtoBaseAccount, // prototype
//		maccPerms,
//	)
//
//	blockedAddr := make(map[string]bool, len(maccPerms))
//	for acc := range maccPerms {
//		blockedAddr[authtypes.NewModuleAddress(acc).String()] = true
//	}
//	bankKeeper := bankkeeper.NewBaseKeeper(
//		marshaler,
//		keyBank,
//		accountKeeper,
//		getSubspace(paramsKeeper, banktypes.ModuleName),
//		blockedAddr,
//	)
//	bankKeeper.SetParams(ctx, banktypes.Params{
//		SendEnabled:        []*banktypes.SendEnabled{},
//		DefaultSendEnabled: true,
//	})
//
//	stakingKeeper := stakingkeeper.NewKeeper(marshaler, keyStaking, accountKeeper, bankKeeper, getSubspace(paramsKeeper, stakingtypes.ModuleName))
//	stakingKeeper.SetParams(ctx, TestingStakeParams)
//
//	distKeeper := distrkeeper.NewKeeper(marshaler, keyDistro, getSubspace(paramsKeeper, distrtypes.ModuleName), accountKeeper, bankKeeper, stakingKeeper, authtypes.FeeCollectorName, nil)
//	distKeeper.SetParams(ctx, distrtypes.DefaultParams())
//
//	// set genesis items required for distribution
//	distKeeper.SetFeePool(ctx, distrtypes.InitialFeePool())
//
//	// total supply to track this
//	totalSupply := sdk.NewCoins(sdk.NewInt64Coin("stake", 100000000))
//
//	// set up initial accounts
//	for name, perms := range maccPerms {
//		mod := authtypes.NewEmptyModuleAccount(name, perms...)
//		if name == stakingtypes.NotBondedPoolName {
//			err = bankKeeper.MintCoins(ctx, types.ModuleName, totalSupply)
//			require.NoError(t, err)
//			err = bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, mod.Name, totalSupply)
//			require.NoError(t, err)
//		} else if name == distrtypes.ModuleName {
//			// some big pot to pay out
//			amt := sdk.NewCoins(sdk.NewInt64Coin("stake", 500000))
//			err = bankKeeper.MintCoins(ctx, types.ModuleName, amt)
//			require.NoError(t, err)
//			err = bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, mod.Name, amt)
//			require.NoError(t, err)
//		}
//		accountKeeper.SetModuleAccount(ctx, mod)
//	}
//
//	stakeAddr := authtypes.NewModuleAddress(stakingtypes.BondedPoolName)
//	moduleAcct := accountKeeper.GetAccount(ctx, stakeAddr)
//	require.NotNil(t, moduleAcct)
//
//	router := baseapp.NewRouter()
//	router.AddRoute(bank.AppModule{
//		AppModuleBasic: bank.AppModuleBasic{},
//	}.Route())
//	router.AddRoute(staking.AppModule{
//		AppModuleBasic: staking.AppModuleBasic{},
//	}.Route())
//	router.AddRoute(distribution.AppModule{
//		AppModuleBasic: distribution.AppModuleBasic{},
//	}.Route())
//
//	slashingKeeper := slashingkeeper.NewKeeper(
//		marshaler,
//		keySlashing,
//		&stakingKeeper,
//		getSubspace(paramsKeeper, slashingtypes.ModuleName).WithKeyTable(slashingtypes.ParamKeyTable()),
//	)
//
//	k := NewKeeper(qgbKey, getSubspace(paramsKeeper, types.DefaultParamspace), marshaler, &bankKeeper, &stakingKeeper, &slashingKeeper, &distKeeper, &accountKeeper)
//
//	stakingKeeper = *stakingKeeper.SetHooks(
//		stakingtypes.NewMultiStakingHooks(
//			distKeeper.Hooks(),
//			slashingKeeper.Hooks(),
//			k.Hooks(),
//		),
//	)
//
//	// set gravityIDs for batches and tx items, simulating genesis setup
//	k.SetLatestValsetNonce(ctx, 0)
//	k.setLastObservedEventNonce(ctx, 0)
//	k.SetLastSlashedValsetNonce(ctx, 0)
//	k.SetLastSlashedBatchBlock(ctx, 0)
//	k.SetLastSlashedLogicCallBlock(ctx, 0)
//	k.setID(ctx, 0, []byte(types.KeyLastTXPoolID))
//	k.setID(ctx, 0, []byte(types.KeyLastOutgoingBatchID))
//
//	k.SetParams(ctx, TestingGravityParams)
//	params := k.GetParams(ctx)
//
//	fmt.Println(params)
//
//	return TestInput{
//		QgbKeeper:      k,
//		AccountKeeper:  accountKeeper,
//		BankKeeper:     bankKeeper,
//		StakingKeeper:  stakingKeeper,
//		SlashingKeeper: slashingKeeper,
//		DistKeeper:     distKeeper,
//		GovKeeper:      govKeeper,
//		Context:        ctx,
//		Marshaler:      marshaler,
//		LegacyAmino:    cdc,
//	}
//}
//
//// MakeTestCodec creates a legacy amino codec for testing
//func MakeTestCodec() *codec.LegacyAmino {
//	var cdc = codec.NewLegacyAmino()
//	auth.AppModuleBasic{}.RegisterLegacyAminoCodec(cdc)
//	bank.AppModuleBasic{}.RegisterLegacyAminoCodec(cdc)
//	staking.AppModuleBasic{}.RegisterLegacyAminoCodec(cdc)
//	distribution.AppModuleBasic{}.RegisterLegacyAminoCodec(cdc)
//	sdk.RegisterLegacyAminoCodec(cdc)
//	ccodec.RegisterCrypto(cdc)
//	params.AppModuleBasic{}.RegisterLegacyAminoCodec(cdc)
//	types.RegisterCodec(cdc)
//	return cdc
//}
//
//// getSubspace returns a param subspace for a given module name.
//func getSubspace(k paramskeeper.Keeper, moduleName string) paramstypes.Subspace {
//	subspace, _ := k.GetSubspace(moduleName)
//	return subspace
//}
//
//// MakeTestMarshaler creates a proto codec for use in testing
//func MakeTestMarshaler() codec.Codec {
//	interfaceRegistry := codectypes.NewInterfaceRegistry()
//	std.RegisterInterfaces(interfaceRegistry)
//	ModuleBasics.RegisterInterfaces(interfaceRegistry)
//	types.RegisterInterfaces(interfaceRegistry)
//	return codec.NewProtoCodec(interfaceRegistry)
//}
