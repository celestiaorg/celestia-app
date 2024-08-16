package util

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/x/blobstream"

	cosmosmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/x/blobstream/keeper"
	blobstreamtypes "github.com/celestiaorg/celestia-app/x/blobstream/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	ccodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	ccrypto "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/std"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/distribution"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/params"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	tmed "github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
	dbm "github.com/tendermint/tm-db"
)

var (
	// ModuleBasics is a mock module basic manager for testing
	ModuleBasics = app.ModuleBasics
	// TestingStakeParams is a set of staking params for testing
	TestingStakeParams = stakingtypes.Params{
		UnbondingTime:     100,
		MaxValidators:     10,
		MaxEntries:        10,
		HistoricalEntries: 10000,
		BondDenom:         "stake",
		MinCommissionRate: sdk.NewDecWithPrec(0, 0),
	}

	// HardcodedConsensusPrivKeys
	FixedConsensusPrivKeys = []tmed.PrivKey{
		tmed.GenPrivKeyFromSecret([]byte("12345678901234567890123456389012")),
		tmed.GenPrivKeyFromSecret([]byte("12345678901234567890123456389013")),
		tmed.GenPrivKeyFromSecret([]byte("12345678901234567890123456389014")),
		tmed.GenPrivKeyFromSecret([]byte("12345678901234567890123456389015")),
		tmed.GenPrivKeyFromSecret([]byte("12345678901234567890123456389016")),
	}

	FixedNetworkPrivKeys = []tmed.PrivKey{
		tmed.GenPrivKeyFromSecret([]byte("12345678901234567890123456786012")),
		tmed.GenPrivKeyFromSecret([]byte("12345678901234567890123456786013")),
		tmed.GenPrivKeyFromSecret([]byte("12345678901234567890123456786014")),
		tmed.GenPrivKeyFromSecret([]byte("12345678901234567890123456786015")),
		tmed.GenPrivKeyFromSecret([]byte("12345678901234567890123456786016")),
	}

	// FixedMnemonics is a set of fixed mnemonics for testing.
	// Account names are: validator1, validator2, validator3, validator4, validator5
	FixedMnemonics = []string{
		"body world north giggle crop reduce height copper damp next verify orphan lens loan adjust inform utility theory now ranch motion opinion crowd fun",
		"body champion street fat bone above office guess waste vivid gift around approve elevator depth fiber alarm usual skirt like organ space antique silk",
		"cheap alpha render punch clap prize duty drive steel situate person radar smooth elegant over chronic wait danger thumb soft letter spatial acquire rough",
		"outdoor ramp suspect office disagree world attend vanish small wish capable fall wall soon damp session emotion chest toss viable meat host clerk truth",
		"ability evidence casino cram weasel chest brush bridge sister blur onion found glad own mansion amateur expect force fun dragon famous alien appear open",
	}

	// ConsPrivKeys generate ed25519 ConsPrivKeys to be used for validator operator keys
	ConsPrivKeys = []ccrypto.PrivKey{
		ed25519.GenPrivKey(),
		ed25519.GenPrivKey(),
		ed25519.GenPrivKey(),
		ed25519.GenPrivKey(),
		ed25519.GenPrivKey(),
	}

	// ConsPubKeys holds the consensus public keys to be used for validator operator keys
	ConsPubKeys = []ccrypto.PubKey{
		ConsPrivKeys[0].PubKey(),
		ConsPrivKeys[1].PubKey(),
		ConsPrivKeys[2].PubKey(),
		ConsPrivKeys[3].PubKey(),
		ConsPrivKeys[4].PubKey(),
	}

	// AccPrivKeys generate secp256k1 pubkeys to be used for account pub keys
	AccPrivKeys = []ccrypto.PrivKey{
		secp256k1.GenPrivKey(),
		secp256k1.GenPrivKey(),
		secp256k1.GenPrivKey(),
		secp256k1.GenPrivKey(),
		secp256k1.GenPrivKey(),
	}

	// AccPubKeys holds the pub keys for the account keys
	AccPubKeys = []ccrypto.PubKey{
		AccPrivKeys[0].PubKey(),
		AccPrivKeys[1].PubKey(),
		AccPrivKeys[2].PubKey(),
		AccPrivKeys[3].PubKey(),
		AccPrivKeys[4].PubKey(),
	}

	// AccAddrs holds the sdk.AccAddresses
	AccAddrs = []sdk.AccAddress{
		sdk.AccAddress(AccPubKeys[0].Address()),
		sdk.AccAddress(AccPubKeys[1].Address()),
		sdk.AccAddress(AccPubKeys[2].Address()),
		sdk.AccAddress(AccPubKeys[3].Address()),
		sdk.AccAddress(AccPubKeys[4].Address()),
	}

	// ValAddrs holds the sdk.ValAddresses
	ValAddrs = []sdk.ValAddress{
		sdk.ValAddress(AccPubKeys[0].Address()),
		sdk.ValAddress(AccPubKeys[1].Address()),
		sdk.ValAddress(AccPubKeys[2].Address()),
		sdk.ValAddress(AccPubKeys[3].Address()),
		sdk.ValAddress(AccPubKeys[4].Address()),
	}

	// EVMAddrs holds etheruem addresses
	EVMAddrs = initEVMAddrs(100)

	// InitTokens holds the number of tokens to initialize an account with
	InitTokens = sdk.TokensFromConsensusPower(110, sdk.DefaultPowerReduction)

	// InitCoins holds the number of coins to initialize an account with
	InitCoins = sdk.NewCoins(sdk.NewCoin(TestingStakeParams.BondDenom, InitTokens))

	// StakingAmount holds the staking power to start a validator with
	StakingAmount = sdk.TokensFromConsensusPower(10, sdk.DefaultPowerReduction)
)

func initEVMAddrs(count int) []gethcommon.Address {
	addresses := make([]gethcommon.Address, count)
	for i := 0; i < count; i++ {
		evmAddr := gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(i + 1)}, gethcommon.AddressLength))
		addresses[i] = evmAddr
	}
	return addresses
}

// TestInput stores the various keepers required to test Blobstream
type TestInput struct {
	BlobstreamKeeper keeper.Keeper
	AccountKeeper    authkeeper.AccountKeeper
	StakingKeeper    stakingkeeper.Keeper
	SlashingKeeper   slashingkeeper.Keeper
	DistKeeper       distrkeeper.Keeper
	BankKeeper       bankkeeper.BaseKeeper
	Context          sdk.Context
	Marshaler        codec.Codec
	LegacyAmino      *codec.LegacyAmino
}

// CreateTestEnvWithoutBlobstreamKeysInit creates the keeper testing environment for Blobstream
func CreateTestEnvWithoutBlobstreamKeysInit(t *testing.T) TestInput {
	t.Helper()

	// Initialize store keys
	keyBlobstream := sdk.NewKVStoreKey(blobstreamtypes.StoreKey)
	keyAuth := sdk.NewKVStoreKey(authtypes.StoreKey)
	keyStaking := sdk.NewKVStoreKey(stakingtypes.StoreKey)
	keyBank := sdk.NewKVStoreKey(banktypes.StoreKey)
	keyDistribution := sdk.NewKVStoreKey(distrtypes.StoreKey)
	keyParams := sdk.NewKVStoreKey(paramstypes.StoreKey)
	tkeyParams := sdk.NewTransientStoreKey(paramstypes.TStoreKey)
	keySlashing := sdk.NewKVStoreKey(slashingtypes.StoreKey)

	// Initialize memory database and mount stores on it
	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	ms.MountStoreWithDB(keyBlobstream, storetypes.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyAuth, storetypes.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyParams, storetypes.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyStaking, storetypes.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyBank, storetypes.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyDistribution, storetypes.StoreTypeIAVL, db)
	ms.MountStoreWithDB(tkeyParams, storetypes.StoreTypeTransient, db)
	ms.MountStoreWithDB(keySlashing, storetypes.StoreTypeIAVL, db)
	err := ms.LoadLatestVersion()
	require.NoError(t, err)

	ctx := sdk.NewContext(ms, tmproto.Header{
		Version: tmversion.Consensus{
			Block: 0,
			App:   0,
		},
		ChainID: "",
		Height:  1234567,
		Time:    time.Date(2020, time.April, 22, 12, 0, 0, 0, time.UTC),
		LastBlockId: tmproto.BlockID{
			Hash: []byte{},
			PartSetHeader: tmproto.PartSetHeader{
				Total: 0,
				Hash:  []byte{},
			},
		},
		LastCommitHash:     []byte{},
		DataHash:           []byte{},
		ValidatorsHash:     []byte{},
		NextValidatorsHash: []byte{},
		ConsensusHash:      []byte{},
		AppHash:            []byte{},
		LastResultsHash:    []byte{},
		EvidenceHash:       []byte{},
		ProposerAddress:    []byte{},
	}, false, log.TestingLogger())

	cdc := MakeTestCodec()
	marshaler := MakeTestMarshaler()

	paramsKeeper := paramskeeper.NewKeeper(marshaler, cdc, keyParams, tkeyParams)
	paramsKeeper.Subspace(authtypes.ModuleName)
	paramsKeeper.Subspace(banktypes.ModuleName)
	paramsKeeper.Subspace(stakingtypes.ModuleName)
	paramsKeeper.Subspace(distrtypes.ModuleName)
	paramsKeeper.Subspace(blobstreamtypes.DefaultParamspace)
	paramsKeeper.Subspace(slashingtypes.ModuleName)

	// this is also used to initialize module accounts for all the map keys
	moduleAccountPermissions := map[string][]string{
		authtypes.FeeCollectorName:     nil,
		distrtypes.ModuleName:          nil,
		stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
		stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
		blobstreamtypes.ModuleName:     {authtypes.Minter, authtypes.Burner},
	}

	accountKeeper := authkeeper.NewAccountKeeper(
		marshaler,
		keyAuth, // target store
		getSubspace(paramsKeeper, authtypes.ModuleName),
		authtypes.ProtoBaseAccount, // prototype
		moduleAccountPermissions,
		app.Bech32PrefixAccAddr,
	)

	blockedAddr := make(map[string]bool, len(moduleAccountPermissions))
	for acc := range moduleAccountPermissions {
		blockedAddr[authtypes.NewModuleAddress(acc).String()] = true
	}
	bankKeeper := bankkeeper.NewBaseKeeper(
		marshaler,
		keyBank,
		accountKeeper,
		getSubspace(paramsKeeper, banktypes.ModuleName),
		blockedAddr,
	)
	bankKeeper.SetParams(
		ctx,
		banktypes.Params{
			SendEnabled:        []*banktypes.SendEnabled{},
			DefaultSendEnabled: true,
		},
	)

	stakingKeeper := stakingkeeper.NewKeeper(marshaler, keyStaking, accountKeeper, bankKeeper, getSubspace(paramsKeeper, stakingtypes.ModuleName))
	stakingKeeper.SetParams(ctx, TestingStakeParams)

	distKeeper := distrkeeper.NewKeeper(marshaler, keyDistribution, getSubspace(paramsKeeper, distrtypes.ModuleName), accountKeeper, bankKeeper, stakingKeeper, authtypes.FeeCollectorName)
	distKeeper.SetParams(ctx, distrtypes.DefaultParams())
	distKeeper.SetFeePool(ctx, distrtypes.InitialFeePool())

	// set up initial accounts
	for name, permissions := range moduleAccountPermissions {
		moduleAccount := authtypes.NewEmptyModuleAccount(name, permissions...)
		totalSupply := sdk.NewCoins(sdk.NewInt64Coin("stake", 100000000))
		if name == stakingtypes.NotBondedPoolName {
			err = bankKeeper.MintCoins(ctx, blobstreamtypes.ModuleName, totalSupply)
			require.NoError(t, err)
			err = bankKeeper.SendCoinsFromModuleToModule(ctx, blobstreamtypes.ModuleName, moduleAccount.Name, totalSupply)
			require.NoError(t, err)
		} else if name == distrtypes.ModuleName {
			// some big pot to pay out
			amt := sdk.NewCoins(sdk.NewInt64Coin("stake", 500000))
			err = bankKeeper.MintCoins(ctx, blobstreamtypes.ModuleName, amt)
			require.NoError(t, err)
			err = bankKeeper.SendCoinsFromModuleToModule(ctx, blobstreamtypes.ModuleName, moduleAccount.Name, amt)
			require.NoError(t, err)
		}
		accountKeeper.SetModuleAccount(ctx, moduleAccount)
	}

	stakeAddr := authtypes.NewModuleAddress(stakingtypes.BondedPoolName)
	moduleAcct := accountKeeper.GetAccount(ctx, stakeAddr)
	require.NotNil(t, moduleAcct)

	slashingKeeper := slashingkeeper.NewKeeper(
		marshaler,
		keySlashing,
		&stakingKeeper,
		getSubspace(paramsKeeper, slashingtypes.ModuleName),
	)

	blobstreamKeeper := keeper.NewKeeper(marshaler, keyBlobstream, getSubspace(paramsKeeper, blobstreamtypes.DefaultParamspace), &stakingKeeper)
	blobstreamKeeper.SetParams(ctx, *blobstreamtypes.DefaultGenesis().Params)

	stakingKeeper = *stakingKeeper.SetHooks(
		stakingtypes.NewMultiStakingHooks(
			distKeeper.Hooks(),
			slashingKeeper.Hooks(),
			blobstreamKeeper.Hooks(),
		),
	)
	return TestInput{
		BlobstreamKeeper: *blobstreamKeeper,
		AccountKeeper:    accountKeeper,
		BankKeeper:       bankKeeper,
		StakingKeeper:    stakingKeeper,
		SlashingKeeper:   slashingKeeper,
		DistKeeper:       distKeeper,
		Context:          ctx,
		Marshaler:        marshaler,
		LegacyAmino:      cdc,
	}
}

// CreateTestEnv creates the keeper testing environment for Blobstream
func CreateTestEnv(t *testing.T) TestInput {
	input := CreateTestEnvWithoutBlobstreamKeysInit(t)
	input.BlobstreamKeeper.SetLatestAttestationNonce(input.Context, blobstream.InitialLatestAttestationNonce)
	input.BlobstreamKeeper.SetEarliestAvailableAttestationNonce(input.Context, blobstream.InitialEarliestAvailableAttestationNonce)
	return input
}

// MakeTestCodec creates a legacy amino codec for testing
func MakeTestCodec() *codec.LegacyAmino {
	cdc := codec.NewLegacyAmino()
	auth.AppModuleBasic{}.RegisterLegacyAminoCodec(cdc)
	bank.AppModuleBasic{}.RegisterLegacyAminoCodec(cdc)
	staking.AppModuleBasic{}.RegisterLegacyAminoCodec(cdc)
	distribution.AppModuleBasic{}.RegisterLegacyAminoCodec(cdc)
	sdk.RegisterLegacyAminoCodec(cdc)
	ccodec.RegisterCrypto(cdc)
	params.AppModuleBasic{}.RegisterLegacyAminoCodec(cdc)
	blobstreamtypes.RegisterLegacyAminoCodec(cdc)
	return cdc
}

// getSubspace returns a param subspace for a given module name.
func getSubspace(k paramskeeper.Keeper, moduleName string) paramstypes.Subspace {
	subspace, _ := k.GetSubspace(moduleName)
	return subspace
}

// MakeTestMarshaler creates a proto codec for use in testing
func MakeTestMarshaler() codec.Codec {
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	ModuleBasics.RegisterInterfaces(interfaceRegistry)
	blobstreamtypes.RegisterInterfaces(interfaceRegistry)
	return codec.NewProtoCodec(interfaceRegistry)
}

// SetupFiveValChain does all the initialization for a 5 Validator chain using the keys here
func SetupFiveValChain(t *testing.T) (TestInput, sdk.Context) {
	t.Helper()
	input := CreateTestEnv(t)

	// Set the params for our modules
	input.StakingKeeper.SetParams(input.Context, TestingStakeParams)

	// Initialize each of the validators
	for i := range []int{0, 1, 2, 3, 4} {
		CreateValidator(t, input, AccAddrs[i], AccPubKeys[i], uint64(i), ValAddrs[i], ConsPubKeys[i], StakingAmount)
		RegisterEVMAddress(t, input, ValAddrs[i], EVMAddrs[i])
	}

	// Run the staking endblocker to ensure valset is correct in state
	staking.EndBlocker(input.Context, input.StakingKeeper)

	// Return the test input
	return input, input.Context
}

func CreateValidator(
	t *testing.T,
	input TestInput,
	accAddr sdk.AccAddress,
	accPubKey ccrypto.PubKey,
	accountNumber uint64,
	valAddr sdk.ValAddress,
	consPubKey ccrypto.PubKey,
	stakingAmount cosmosmath.Int,
) {
	// Initialize the account for the key
	acc := input.AccountKeeper.NewAccount(
		input.Context,
		authtypes.NewBaseAccount(accAddr, accPubKey, accountNumber, 0),
	)

	// Set the balance for the account
	require.NoError(t, input.BankKeeper.MintCoins(input.Context, blobstreamtypes.ModuleName, InitCoins))
	err := input.BankKeeper.SendCoinsFromModuleToAccount(input.Context, blobstreamtypes.ModuleName, acc.GetAddress(), InitCoins)
	require.NoError(t, err)

	// Set the account in state
	input.AccountKeeper.SetAccount(input.Context, acc)

	// Create a validator for that account using some tokens in the account
	// and the staking handler
	msgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)
	_, err = msgServer.CreateValidator(input.Context, NewTestMsgCreateValidator(valAddr, consPubKey, stakingAmount))
	require.NoError(t, err)
}

func RegisterEVMAddress(
	t *testing.T,
	input TestInput,
	valAddr sdk.ValAddress,
	evmAddr gethcommon.Address,
) {
	bsMsgServer := keeper.NewMsgServerImpl(input.BlobstreamKeeper)
	registerMsg := blobstreamtypes.NewMsgRegisterEVMAddress(valAddr, evmAddr)
	_, err := bsMsgServer.RegisterEVMAddress(input.Context, registerMsg)
	require.NoError(t, err)
}

func NewTestMsgCreateValidator(
	address sdk.ValAddress,
	pubKey ccrypto.PubKey,
	amt cosmosmath.Int,
) *stakingtypes.MsgCreateValidator {
	commission := stakingtypes.NewCommissionRates(sdk.ZeroDec(), sdk.ZeroDec(), sdk.ZeroDec())
	out, err := stakingtypes.NewMsgCreateValidator(
		address, pubKey, sdk.NewCoin("stake", amt),
		stakingtypes.Description{
			Moniker:         "",
			Identity:        "",
			Website:         "",
			SecurityContact: "",
			Details:         "",
		}, commission, sdk.OneInt(),
	)
	if err != nil {
		panic(err)
	}
	return out
}

// SetupTestChain sets up a test environment with the provided validator voting weights
func SetupTestChain(t *testing.T, weights []uint64) (TestInput, sdk.Context) {
	t.Helper()
	input := CreateTestEnv(t)

	// Set the params for our modules
	TestingStakeParams.MaxValidators = 100
	input.StakingKeeper.SetParams(input.Context, TestingStakeParams)

	// Initialize each of the validators
	stakingMsgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)
	bsMsgServer := keeper.NewMsgServerImpl(input.BlobstreamKeeper)
	for i, weight := range weights {
		consPrivKey := ed25519.GenPrivKey()
		consPubKey := consPrivKey.PubKey()
		valPrivKey := secp256k1.GenPrivKey()
		valPubKey := valPrivKey.PubKey()
		valAddr := sdk.ValAddress(valPubKey.Address())
		accAddr := sdk.AccAddress(valPubKey.Address())

		// Initialize the account for the key
		acc := input.AccountKeeper.NewAccount(
			input.Context,
			authtypes.NewBaseAccount(accAddr, valPubKey, uint64(i), 0),
		)

		// Set the balance for the account
		weightCoins := sdk.NewCoins(sdk.NewInt64Coin(TestingStakeParams.BondDenom, int64(weight)))
		require.NoError(t, input.BankKeeper.MintCoins(input.Context, blobstreamtypes.ModuleName, weightCoins))
		require.NoError(t, input.BankKeeper.SendCoinsFromModuleToAccount(input.Context, blobstreamtypes.ModuleName, accAddr, weightCoins))

		// Set the account in state
		input.AccountKeeper.SetAccount(input.Context, acc)

		// Create a validator for that account using some of the tokens in the account
		// and the staking handler
		_, err := stakingMsgServer.CreateValidator(
			input.Context,
			NewTestMsgCreateValidator(valAddr, consPubKey, sdk.NewIntFromUint64(weight)),
		)
		require.NoError(t, err)

		registerMsg := blobstreamtypes.NewMsgRegisterEVMAddress(valAddr, EVMAddrs[i])
		_, err = bsMsgServer.RegisterEVMAddress(input.Context, registerMsg)
		require.NoError(t, err)

		// Run the staking endblocker to ensure valset is correct in state
		staking.EndBlocker(input.Context, input.StakingKeeper)
	}

	// some inputs can cause the validator creation not to work, this checks that
	// everything was successful
	validators := input.StakingKeeper.GetBondedValidatorsByPower(input.Context)
	require.Equal(t, len(weights), len(validators))

	// Return the test input
	return input, input.Context
}

func NewTestMsgUnDelegateValidator(address sdk.ValAddress, amt cosmosmath.Int) *stakingtypes.MsgUndelegate {
	msg := stakingtypes.NewMsgUndelegate(sdk.AccAddress(address), address, sdk.NewCoin("stake", amt))
	return msg
}

// ExecuteBlobstreamHeights executes the end exclusive range of heights specified by beginHeight and endHeight
// along with the Blobstream abci.EndBlocker on each one of them.
// Returns the updated context with block height advanced to endHeight.
func ExecuteBlobstreamHeights(ctx sdk.Context, bsKeeper keeper.Keeper, beginHeight int64, endHeight int64) sdk.Context {
	for i := beginHeight; i < endHeight; i++ {
		ctx = ctx.WithBlockHeight(i)
		blobstream.EndBlocker(ctx, bsKeeper)
	}
	return ctx
}

// ExecuteBlobstreamHeightsWithTime executes the end exclusive range of heights specified by beginHeight and endHeight
// along with the Blobstream abci.EndBlocker on each one of them.
// Uses the interval to calculate the block header time.
func ExecuteBlobstreamHeightsWithTime(ctx sdk.Context, bsKeeper keeper.Keeper, beginHeight int64, endHeight int64, blockInterval time.Duration) sdk.Context {
	blockTime := ctx.BlockTime()
	for i := beginHeight; i < endHeight; i++ {
		ctx = ctx.WithBlockHeight(i).WithBlockTime(blockTime)
		blobstream.EndBlocker(ctx, bsKeeper)
		blockTime = blockTime.Add(blockInterval)
	}
	return ctx
}
