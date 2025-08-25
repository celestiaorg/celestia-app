package keeper_test

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (suite *KeeperTestSuite) TestQueryFibreProviderInfo_Found() {
	validatorAddr := sdk.ValAddress("validator1")
	
	// Set fibre provider info
	info := types.FibreProviderInfo{IpAddress: "192.168.1.1"}
	suite.keeper.SetFibreProviderInfo(suite.ctx, validatorAddr, info)
	
	// Query the info
	req := &types.QueryFibreProviderInfoRequest{
		ValidatorAddress: validatorAddr.String(),
	}
	
	resp, err := suite.keeper.FibreProviderInfo(suite.ctx, req)
	suite.NoError(err)
	suite.NotNil(resp)
	suite.True(resp.Found)
	suite.NotNil(resp.Info)
	suite.Equal("192.168.1.1", resp.Info.IpAddress)
}

func (suite *KeeperTestSuite) TestQueryFibreProviderInfo_NotFound() {
	validatorAddr := sdk.ValAddress("validator1")
	
	// Query non-existent info
	req := &types.QueryFibreProviderInfoRequest{
		ValidatorAddress: validatorAddr.String(),
	}
	
	resp, err := suite.keeper.FibreProviderInfo(suite.ctx, req)
	suite.NoError(err)
	suite.NotNil(resp)
	suite.False(resp.Found)
	suite.NotNil(resp.Info) // Should be empty struct, not nil
	suite.Equal("", resp.Info.IpAddress)
}

func (suite *KeeperTestSuite) TestQueryFibreProviderInfo_InvalidRequest() {
	// Test nil request
	resp, err := suite.keeper.FibreProviderInfo(context.Background(), nil)
	suite.Error(err)
	suite.Nil(resp)
	suite.Contains(err.Error(), "invalid request")
}

func (suite *KeeperTestSuite) TestQueryFibreProviderInfo_EmptyValidatorAddress() {
	// Test empty validator address
	req := &types.QueryFibreProviderInfoRequest{
		ValidatorAddress: "",
	}
	
	resp, err := suite.keeper.FibreProviderInfo(suite.ctx, req)
	suite.Error(err)
	suite.Nil(resp)
	suite.Contains(err.Error(), "validator address cannot be empty")
}

func (suite *KeeperTestSuite) TestQueryFibreProviderInfo_InvalidValidatorAddress() {
	// Test invalid validator address
	req := &types.QueryFibreProviderInfoRequest{
		ValidatorAddress: "invalid-address",
	}
	
	resp, err := suite.keeper.FibreProviderInfo(suite.ctx, req)
	suite.Error(err)
	suite.Nil(resp)
	suite.Contains(err.Error(), "invalid validator address")
}

func (suite *KeeperTestSuite) TestQueryAllActiveFibreProviders_Success() {
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
	
	// Query all active providers
	req := &types.QueryAllActiveFibreProvidersRequest{}
	
	resp, err := suite.keeper.AllActiveFibreProviders(suite.ctx, req)
	suite.NoError(err)
	suite.NotNil(resp)
	
	// Should only return bonded validators (val1 and val2)
	suite.Len(resp.Providers, 2)
	
	// Check that only active validators are returned
	foundVal1, foundVal2 := false, false
	for _, provider := range resp.Providers {
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

func (suite *KeeperTestSuite) TestQueryAllActiveFibreProviders_NoActiveProviders() {
	// Set up an unbonded validator with fibre info
	val1Addr := sdk.ValAddress("validator1")
	val1 := stakingtypes.Validator{
		OperatorAddress: val1Addr.String(),
		Status:         stakingtypes.Unbonded,
	}
	suite.stakingKeeper.setUnbondedValidator(val1)
	
	info1 := types.FibreProviderInfo{IpAddress: "192.168.1.1"}
	suite.keeper.SetFibreProviderInfo(suite.ctx, val1Addr, info1)
	
	// Query all active providers
	req := &types.QueryAllActiveFibreProvidersRequest{}
	
	resp, err := suite.keeper.AllActiveFibreProviders(suite.ctx, req)
	suite.NoError(err)
	suite.NotNil(resp)
	
	// Should return empty list since no validators are active
	suite.Len(resp.Providers, 0)
}

func (suite *KeeperTestSuite) TestQueryAllActiveFibreProviders_ActiveWithoutInfo() {
	// Set up bonded validator without fibre info
	val1Addr := sdk.ValAddress("validator1")
	val1 := stakingtypes.Validator{
		OperatorAddress: val1Addr.String(),
		Status:         stakingtypes.Bonded,
	}
	suite.stakingKeeper.setBondedValidator(val1)
	
	// Query all active providers (no fibre info set)
	req := &types.QueryAllActiveFibreProvidersRequest{}
	
	resp, err := suite.keeper.AllActiveFibreProviders(suite.ctx, req)
	suite.NoError(err)
	suite.NotNil(resp)
	
	// Should return empty list since validator has no fibre info
	suite.Len(resp.Providers, 0)
}

func (suite *KeeperTestSuite) TestQueryAllActiveFibreProviders_WithPagination() {
	// Set up multiple bonded validators with fibre info
	validators := []string{"validator1", "validator2", "validator3"}
	for i, valName := range validators {
		valAddr := sdk.ValAddress(valName)
		val := stakingtypes.Validator{
			OperatorAddress: valAddr.String(),
			Status:         stakingtypes.Bonded,
		}
		suite.stakingKeeper.setBondedValidator(val)
		
		info := types.FibreProviderInfo{
			IpAddress: fmt.Sprintf("192.168.1.%d", i+1),
		}
		suite.keeper.SetFibreProviderInfo(suite.ctx, valAddr, info)
	}
	
	// Query with pagination (page 1, limit 2)
	req := &types.QueryAllActiveFibreProvidersRequest{
		Pagination: &query.PageRequest{
			Limit:  2,
			Offset: 0,
		},
	}
	
	resp, err := suite.keeper.AllActiveFibreProviders(suite.ctx, req)
	suite.NoError(err)
	suite.NotNil(resp)
	suite.Len(resp.Providers, 2)
	suite.Equal(uint64(3), resp.Pagination.Total)

	// Query with pagination (page 2, limit 2)
	req = &types.QueryAllActiveFibreProvidersRequest{
		Pagination: &query.PageRequest{
			Limit:  2,
			Offset: 2,
		},
	}

	resp, err = suite.keeper.AllActiveFibreProviders(suite.ctx, req)
	suite.NoError(err)
	suite.NotNil(resp)
	suite.Len(resp.Providers, 1)
	suite.Equal(uint64(3), resp.Pagination.Total)
}

func (suite *KeeperTestSuite) TestQueryAllActiveFibreProviders_NilRequest() {
	// Test nil request
	resp, err := suite.keeper.AllActiveFibreProviders(context.Background(), nil)
	suite.Error(err)
	suite.Nil(resp)
	suite.Contains(err.Error(), "invalid request")
}