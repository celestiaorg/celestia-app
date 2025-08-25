package keeper_test

import (
	"github.com/celestiaorg/celestia-app/v6/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (suite *KeeperTestSuite) TestMsgSetFibreProviderInfo_Success() {
	msgServer := keeper.NewMsgServerImpl(suite.keeper)
	validatorAddr := sdk.ValAddress("validator1")
	
	// Set up bonded validator
	val := stakingtypes.Validator{
		OperatorAddress: validatorAddr.String(),
		Status:         stakingtypes.Bonded,
	}
	suite.stakingKeeper.setBondedValidator(val)
	
	// Create message
	msg := &types.MsgSetFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		IpAddress:        "192.168.1.1",
	}
	
	// Execute message
	_, err := msgServer.SetFibreProviderInfo(suite.ctx, msg)
	suite.NoError(err)
	
	// Verify info was set
	info, found := suite.keeper.GetFibreProviderInfo(suite.ctx, validatorAddr)
	suite.True(found)
	suite.Equal("192.168.1.1", info.IpAddress)
	
	// Verify event was emitted
	events := suite.ctx.EventManager().Events()
	suite.Len(events, 1)
	
	event := events[0]
	suite.Equal(types.EventTypeSetFibreProviderInfo, event.Type)
	
	// Check event attributes
	attrs := event.Attributes
	suite.Len(attrs, 2)
	suite.Equal(types.AttributeValidatorAddress, attrs[0].Key)
	suite.Equal(validatorAddr.String(), attrs[0].Value)
	suite.Equal(types.AttributeIPAddress, attrs[1].Key)
	suite.Equal("192.168.1.1", attrs[1].Value)
}

func (suite *KeeperTestSuite) TestMsgSetFibreProviderInfo_InvalidValidatorAddress() {
	msgServer := keeper.NewMsgServerImpl(suite.keeper)
	
	// Create message with invalid validator address
	msg := &types.MsgSetFibreProviderInfo{
		ValidatorAddress: "invalid-address",
		IpAddress:        "192.168.1.1",
	}
	
	// Execute message - should fail
	_, err := msgServer.SetFibreProviderInfo(suite.ctx, msg)
	suite.Error(err)
	suite.Contains(err.Error(), "invalid validator address")
}

func (suite *KeeperTestSuite) TestMsgSetFibreProviderInfo_ValidatorNotActive() {
	msgServer := keeper.NewMsgServerImpl(suite.keeper)
	validatorAddr := sdk.ValAddress("validator1")
	
	// Set up unbonded validator
	val := stakingtypes.Validator{
		OperatorAddress: validatorAddr.String(),
		Status:         stakingtypes.Unbonded,
	}
	suite.stakingKeeper.setUnbondedValidator(val)
	
	// Create message
	msg := &types.MsgSetFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		IpAddress:        "192.168.1.1",
	}
	
	// Execute message - should fail
	_, err := msgServer.SetFibreProviderInfo(suite.ctx, msg)
	suite.Error(err)
	suite.Equal(types.ErrValidatorNotActive, err)
}

func (suite *KeeperTestSuite) TestMsgSetFibreProviderInfo_ValidatorNotFound() {
	msgServer := keeper.NewMsgServerImpl(suite.keeper)
	validatorAddr := sdk.ValAddress("nonexistent")
	
	// Create message for non-existent validator
	msg := &types.MsgSetFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		IpAddress:        "192.168.1.1",
	}
	
	// Execute message - should fail
	_, err := msgServer.SetFibreProviderInfo(suite.ctx, msg)
	suite.Error(err)
	suite.Contains(err.Error(), "error checking validator status")
}

func (suite *KeeperTestSuite) TestMsgRemoveFibreProviderInfo_Success() {
	msgServer := keeper.NewMsgServerImpl(suite.keeper)
	validatorAddr := sdk.ValAddress("validator1")
	removerAddr := sdk.AccAddress("remover1")
	
	// Set up unbonded validator with existing fibre info
	val := stakingtypes.Validator{
		OperatorAddress: validatorAddr.String(),
		Status:         stakingtypes.Unbonded,
	}
	suite.stakingKeeper.setUnbondedValidator(val)
	
	// Set initial fibre provider info
	info := types.FibreProviderInfo{IpAddress: "192.168.1.1"}
	suite.keeper.SetFibreProviderInfo(suite.ctx, validatorAddr, info)
	
	// Verify info exists
	suite.True(suite.keeper.HasFibreProviderInfo(suite.ctx, validatorAddr))
	
	// Create remove message
	msg := &types.MsgRemoveFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		RemoverAddress:   removerAddr.String(),
	}
	
	// Execute message
	_, err := msgServer.RemoveFibreProviderInfo(suite.ctx, msg)
	suite.NoError(err)
	
	// Verify info was removed
	suite.False(suite.keeper.HasFibreProviderInfo(suite.ctx, validatorAddr))
	
	// Verify event was emitted
	events := suite.ctx.EventManager().Events()
	suite.Len(events, 1)
	
	event := events[0]
	suite.Equal(types.EventTypeRemoveFibreProviderInfo, event.Type)
	
	// Check event attributes
	attrs := event.Attributes
	suite.Len(attrs, 2)
	suite.Equal(types.AttributeValidatorAddress, attrs[0].Key)
	suite.Equal(validatorAddr.String(), attrs[0].Value)
	suite.Equal(types.AttributeRemoverAddress, attrs[1].Key)
	suite.Equal(removerAddr.String(), attrs[1].Value)
}

func (suite *KeeperTestSuite) TestMsgRemoveFibreProviderInfo_InvalidValidatorAddress() {
	msgServer := keeper.NewMsgServerImpl(suite.keeper)
	removerAddr := sdk.AccAddress("remover1")
	
	// Create message with invalid validator address
	msg := &types.MsgRemoveFibreProviderInfo{
		ValidatorAddress: "invalid-address",
		RemoverAddress:   removerAddr.String(),
	}
	
	// Execute message - should fail
	_, err := msgServer.RemoveFibreProviderInfo(suite.ctx, msg)
	suite.Error(err)
	suite.Contains(err.Error(), "invalid validator address")
}

func (suite *KeeperTestSuite) TestMsgRemoveFibreProviderInfo_ProviderInfoNotFound() {
	msgServer := keeper.NewMsgServerImpl(suite.keeper)
	validatorAddr := sdk.ValAddress("validator1")
	removerAddr := sdk.AccAddress("remover1")
	
	// Set up unbonded validator without fibre info
	val := stakingtypes.Validator{
		OperatorAddress: validatorAddr.String(),
		Status:         stakingtypes.Unbonded,
	}
	suite.stakingKeeper.setUnbondedValidator(val)
	
	// Create remove message
	msg := &types.MsgRemoveFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		RemoverAddress:   removerAddr.String(),
	}
	
	// Execute message - should fail
	_, err := msgServer.RemoveFibreProviderInfo(suite.ctx, msg)
	suite.Error(err)
	suite.Equal(types.ErrProviderInfoNotFound, err)
}

func (suite *KeeperTestSuite) TestMsgRemoveFibreProviderInfo_ValidatorStillActive() {
	msgServer := keeper.NewMsgServerImpl(suite.keeper)
	validatorAddr := sdk.ValAddress("validator1")
	removerAddr := sdk.AccAddress("remover1")
	
	// Set up bonded validator with existing fibre info
	val := stakingtypes.Validator{
		OperatorAddress: validatorAddr.String(),
		Status:         stakingtypes.Bonded,
	}
	suite.stakingKeeper.setBondedValidator(val)
	
	// Set initial fibre provider info
	info := types.FibreProviderInfo{IpAddress: "192.168.1.1"}
	suite.keeper.SetFibreProviderInfo(suite.ctx, validatorAddr, info)
	
	// Create remove message
	msg := &types.MsgRemoveFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		RemoverAddress:   removerAddr.String(),
	}
	
	// Execute message - should fail because validator is still active
	_, err := msgServer.RemoveFibreProviderInfo(suite.ctx, msg)
	suite.Error(err)
	suite.Equal(types.ErrValidatorStillActive, err)
	
	// Verify info still exists
	suite.True(suite.keeper.HasFibreProviderInfo(suite.ctx, validatorAddr))
}

func (suite *KeeperTestSuite) TestMsgRemoveFibreProviderInfo_ValidatorNotFound() {
	msgServer := keeper.NewMsgServerImpl(suite.keeper)
	validatorAddr := sdk.ValAddress("nonexistent")
	removerAddr := sdk.AccAddress("remover1")
	
	// Set fibre info for non-existent validator (this shouldn't happen in practice)
	info := types.FibreProviderInfo{IpAddress: "192.168.1.1"}
	suite.keeper.SetFibreProviderInfo(suite.ctx, validatorAddr, info)
	
	// Create remove message
	msg := &types.MsgRemoveFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		RemoverAddress:   removerAddr.String(),
	}
	
	// Execute message - should fail
	_, err := msgServer.RemoveFibreProviderInfo(suite.ctx, msg)
	suite.Error(err)
	suite.Contains(err.Error(), "error checking validator status")
}