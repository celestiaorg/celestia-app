package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmversion "github.com/cometbft/cometbft/proto/tendermint/version"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type mockStakingKeeper struct {
	validators map[string]stakingtypes.Validator
	bondedValidators []stakingtypes.Validator
}

func (m *mockStakingKeeper) GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	if val, exists := m.validators[addr.String()]; exists {
		return val, nil
	}
	return stakingtypes.Validator{}, stakingtypes.ErrNoValidatorFound
}

func (m *mockStakingKeeper) GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error) {
	return m.bondedValidators, nil
}

func (m *mockStakingKeeper) setBondedValidator(val stakingtypes.Validator) {
	val.Status = stakingtypes.Bonded
	m.validators[val.OperatorAddress] = val
	m.bondedValidators = append(m.bondedValidators, val)
}

func (m *mockStakingKeeper) setUnbondedValidator(val stakingtypes.Validator) {
	val.Status = stakingtypes.Unbonded
	m.validators[val.OperatorAddress] = val
	// Remove from bonded if exists
	for i, v := range m.bondedValidators {
		if v.OperatorAddress == val.OperatorAddress {
			m.bondedValidators = append(m.bondedValidators[:i], m.bondedValidators[i+1:]...)
			break
		}
	}
}

type KeeperTestSuite struct {
	suite.Suite

	ctx    sdk.Context
	keeper keeper.Keeper
	stakingKeeper *mockStakingKeeper
}

func (suite *KeeperTestSuite) SetupTest() {
	fibreStoreKey := storetypes.NewKVStoreKey(types.StoreKey)
	
	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NoOpMetrics{})
	stateStore.MountStoreWithDB(fibreStoreKey, storetypes.StoreTypeIAVL, db)
	require.NoError(suite.T(), stateStore.LoadLatestVersion())
	
	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	
	suite.ctx = sdk.NewContext(stateStore, tmproto.Header{
		Version: tmversion.Consensus{
			Block: 1,
			App:   1,
		},
	}, false, nil)
	
	suite.stakingKeeper = &mockStakingKeeper{
		validators: make(map[string]stakingtypes.Validator),
		bondedValidators: []stakingtypes.Validator{},
	}
	
	suite.keeper = *keeper.NewKeeper(
		cdc,
		fibreStoreKey,
		suite.stakingKeeper,
	)
}

func (suite *KeeperTestSuite) TestSetAndGetFibreProviderInfo() {
	validatorAddr := sdk.ValAddress("validator1")
	info := types.FibreProviderInfo{
		IpAddress: "192.168.1.1",
	}
	
	// Test setting info
	suite.keeper.SetFibreProviderInfo(suite.ctx, validatorAddr, info)
	
	// Test getting info
	retrievedInfo, found := suite.keeper.GetFibreProviderInfo(suite.ctx, validatorAddr)
	suite.True(found)
	suite.Equal(info.IpAddress, retrievedInfo.IpAddress)
}

func (suite *KeeperTestSuite) TestHasFibreProviderInfo() {
	validatorAddr := sdk.ValAddress("validator1")
	
	// Test non-existent info
	suite.False(suite.keeper.HasFibreProviderInfo(suite.ctx, validatorAddr))
	
	// Add info
	info := types.FibreProviderInfo{
		IpAddress: "192.168.1.1",
	}
	suite.keeper.SetFibreProviderInfo(suite.ctx, validatorAddr, info)
	
	// Test existing info
	suite.True(suite.keeper.HasFibreProviderInfo(suite.ctx, validatorAddr))
}

func (suite *KeeperTestSuite) TestRemoveFibreProviderInfo() {
	validatorAddr := sdk.ValAddress("validator1")
	info := types.FibreProviderInfo{
		IpAddress: "192.168.1.1",
	}
	
	// Set info
	suite.keeper.SetFibreProviderInfo(suite.ctx, validatorAddr, info)
	suite.True(suite.keeper.HasFibreProviderInfo(suite.ctx, validatorAddr))
	
	// Remove info
	suite.keeper.RemoveFibreProviderInfo(suite.ctx, validatorAddr)
	suite.False(suite.keeper.HasFibreProviderInfo(suite.ctx, validatorAddr))
	
	// Try to get removed info
	_, found := suite.keeper.GetFibreProviderInfo(suite.ctx, validatorAddr)
	suite.False(found)
}

func (suite *KeeperTestSuite) TestIsValidatorActive() {
	validatorAddr := sdk.ValAddress("validator1")
	
	// Test non-existent validator
	isActive, err := suite.keeper.IsValidatorActive(suite.ctx, validatorAddr)
	suite.Error(err)
	suite.False(isActive)
	
	// Set up bonded validator
	val := stakingtypes.Validator{
		OperatorAddress: validatorAddr.String(),
		Status:         stakingtypes.Bonded,
	}
	suite.stakingKeeper.setBondedValidator(val)
	
	// Test bonded validator
	isActive, err = suite.keeper.IsValidatorActive(suite.ctx, validatorAddr)
	suite.NoError(err)
	suite.True(isActive)
	
	// Set validator as unbonded
	suite.stakingKeeper.setUnbondedValidator(val)
	
	// Test unbonded validator
	isActive, err = suite.keeper.IsValidatorActive(suite.ctx, validatorAddr)
	suite.NoError(err)
	suite.False(isActive)
}

func (suite *KeeperTestSuite) TestGetAllActiveFibreProviders() {
	// Set up multiple validators
	val1Addr := sdk.ValAddress("validator1")
	val2Addr := sdk.ValAddress("validator2")
	val3Addr := sdk.ValAddress("validator3")
	
	val1 := stakingtypes.Validator{
		OperatorAddress: val1Addr.String(),
		Status:         stakingtypes.Bonded,
	}
	val2 := stakingtypes.Validator{
		OperatorAddress: val2Addr.String(),
		Status:         stakingtypes.Bonded,
	}
	val3 := stakingtypes.Validator{
		OperatorAddress: val3Addr.String(),
		Status:         stakingtypes.Unbonded,
	}
	
	suite.stakingKeeper.setBondedValidator(val1)
	suite.stakingKeeper.setBondedValidator(val2)
	suite.stakingKeeper.setUnbondedValidator(val3)
	
	// Set fibre provider info for all validators
	info1 := types.FibreProviderInfo{IpAddress: "192.168.1.1"}
	info2 := types.FibreProviderInfo{IpAddress: "192.168.1.2"}
	info3 := types.FibreProviderInfo{IpAddress: "192.168.1.3"}
	
	suite.keeper.SetFibreProviderInfo(suite.ctx, val1Addr, info1)
	suite.keeper.SetFibreProviderInfo(suite.ctx, val2Addr, info2)
	suite.keeper.SetFibreProviderInfo(suite.ctx, val3Addr, info3)
	
	// Get all active providers
	providers, err := suite.keeper.GetAllActiveFibreProviders(suite.ctx)
	suite.NoError(err)
	
	// Should only return bonded validators (val1 and val2)
	suite.Len(providers, 2)
	
	// Check that only active validators are returned
	foundVal1, foundVal2 := false, false
	for _, provider := range providers {
		if provider.ValidatorAddress == val1.OperatorAddress {
			foundVal1 = true
			suite.Equal("192.168.1.1", provider.Info.IpAddress)
		} else if provider.ValidatorAddress == val2.OperatorAddress {
			foundVal2 = true
			suite.Equal("192.168.1.2", provider.Info.IpAddress)
		}
	}
	suite.True(foundVal1)
	suite.True(foundVal2)
}

func (suite *KeeperTestSuite) TestIterateAllFibreProviderInfo() {
	// Set up multiple validators with fibre info
	val1Addr := sdk.ValAddress("validator1")
	val2Addr := sdk.ValAddress("validator2")
	
	info1 := types.FibreProviderInfo{IpAddress: "192.168.1.1"}
	info2 := types.FibreProviderInfo{IpAddress: "192.168.1.2"}
	
	suite.keeper.SetFibreProviderInfo(suite.ctx, val1Addr, info1)
	suite.keeper.SetFibreProviderInfo(suite.ctx, val2Addr, info2)
	
	// Iterate and collect results
	collected := make(map[string]types.FibreProviderInfo)
	suite.keeper.IterateAllFibreProviderInfo(suite.ctx, func(validatorAddr string, info types.FibreProviderInfo) bool {
		collected[validatorAddr] = info
		return false // continue iteration
	})
	
	// Verify results
	suite.Len(collected, 2)
	suite.Equal("192.168.1.1", collected[val1Addr.String()].IpAddress)
	suite.Equal("192.168.1.2", collected[val2Addr.String()].IpAddress)
}

func (suite *KeeperTestSuite) TestIterateAllFibreProviderInfoEarlyStop() {
	// Set up multiple validators with fibre info
	val1Addr := sdk.ValAddress("validator1")
	val2Addr := sdk.ValAddress("validator2")
	
	info1 := types.FibreProviderInfo{IpAddress: "192.168.1.1"}
	info2 := types.FibreProviderInfo{IpAddress: "192.168.1.2"}
	
	suite.keeper.SetFibreProviderInfo(suite.ctx, val1Addr, info1)
	suite.keeper.SetFibreProviderInfo(suite.ctx, val2Addr, info2)
	
	// Iterate and stop after first item
	collected := make(map[string]types.FibreProviderInfo)
	suite.keeper.IterateAllFibreProviderInfo(suite.ctx, func(validatorAddr string, info types.FibreProviderInfo) bool {
		collected[validatorAddr] = info
		return true // stop iteration
	})
	
	// Should only collect one item
	suite.Len(collected, 1)
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}