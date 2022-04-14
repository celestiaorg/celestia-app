package qgb

import (
	"crypto/ecdsa"
	"encoding/hex"
	"github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// TestMsgValsetConfirm ensures that the valset confirm message sets a validator set confirm
// in the store
func TestMsgValsetConfirm(t *testing.T) {
	var (
		blockTime         = time.Date(2020, 9, 14, 15, 20, 10, 0, time.UTC)
		blockHeight int64 = 200
		signature         = "7c331bd8f2f586b04a2e2cafc6542442ef52e8b8be49533fa6b8962e822bc01e295a62733abfd65a412a8de8286f2794134c160c27a2827bdb71044b94b003cc1c"
	)

	input, ctx := keeper.SetupFiveValChain(t)
	k := input.QgbKeeper
	h := NewHandler(*input.QgbKeeper)

	// set a validator set in the store
	vs, err := k.GetCurrentValset(ctx)
	require.NoError(t, err)
	vs.Height = uint64(1)
	vs.Nonce = uint64(1)
	k.StoreValset(ctx, vs)

	// try wrong eth address
	msg := &types.MsgValsetConfirm{
		Nonce:        1,
		Orchestrator: keeper.OrchAddrs[0].String(),
		EthAddress:   keeper.EthAddrs[1].GetAddress(), // wrong because validator 0 should have EthAddrs[0]
		Signature:    signature,
	}
	ctx = ctx.WithBlockTime(blockTime).WithBlockHeight(blockHeight)
	_, err = h(ctx, msg)
	require.Error(t, err)

	// try a nonexisting valset
	msg = &types.MsgValsetConfirm{
		Nonce:        10,
		Orchestrator: keeper.OrchAddrs[0].String(),
		EthAddress:   keeper.EthAddrs[0].GetAddress(),
		Signature:    signature,
	}
	ctx = ctx.WithBlockTime(blockTime).WithBlockHeight(blockHeight)
	_, err = h(ctx, msg)
	require.Error(t, err)

	msg = &types.MsgValsetConfirm{
		Nonce:        1,
		Orchestrator: keeper.OrchAddrs[0].String(),
		EthAddress:   keeper.EthAddrs[0].GetAddress(),
		Signature:    signature,
	}
	ctx = ctx.WithBlockTime(blockTime).WithBlockHeight(blockHeight)
	_, err = h(ctx, msg)
	require.NoError(t, err)
}

// TestMsgDataCommitmentConfirm ensures that the data commitment confirm message sets a commitment in the store
func TestMsgDataCommitmentConfirm(t *testing.T) {
	var (
		blockTime = time.Date(2020, 9, 14, 15, 20, 10, 0, time.UTC)

		validatorAccPrivateKey = secp256k1.GenPrivKey()
		validatorAccPublicKey  = validatorAccPrivateKey.PubKey()
		validatorAccAddress    = sdk.AccAddress(validatorAccPublicKey.Address())
		validatorValAddress    = sdk.ValAddress(validatorAccPublicKey.Address())

		orchEthPrivateKey, _ = crypto.GenerateKey()
		orchEthPublicKey     = orchEthPrivateKey.Public().(*ecdsa.PublicKey)
		orchEthAddress       = crypto.PubkeyToAddress(*orchEthPublicKey).Hex()
		ethAddr, _           = stakingtypes.NewEthAddress(orchEthAddress)

		orchPrivateKey = secp256k1.GenPrivKey()
		orchPublicKey  = orchPrivateKey.PubKey()
		orchAddress    = sdk.AccAddress(orchPublicKey.Address())
	)

	// Init chain
	input, ctx := keeper.SetupFiveValChain(t)
	k := input.QgbKeeper

	// Create a new validator
	acc := input.AccountKeeper.NewAccount(
		input.Context,
		authtypes.NewBaseAccount(validatorAccAddress, validatorAccPublicKey, uint64(120), 0),
	)
	require.NoError(t, input.BankKeeper.MintCoins(input.Context, types.ModuleName, keeper.InitCoins))
	// nolint
	input.BankKeeper.SendCoinsFromModuleToAccount(input.Context, types.ModuleName, acc.GetAddress(), keeper.InitCoins)
	input.AccountKeeper.SetAccount(input.Context, acc)

	sh := staking.NewHandler(input.StakingKeeper)
	_, err := sh(
		input.Context,
		keeper.NewTestMsgCreateValidator(validatorValAddress, validatorAccPublicKey, keeper.StakingAmount, orchAddress, *ethAddr),
	)
	require.NoError(t, err)
	staking.EndBlocker(input.Context, input.StakingKeeper)

	h := NewHandler(*input.QgbKeeper)
	ctx = ctx.WithBlockTime(blockTime)

	// Signs the commitment using the orth eth private key
	signature, err := types.NewEthereumSignature([]byte("commitment"), orchEthPrivateKey)
	if err != nil {
		panic(err)
	}

	// Sending a data commitment confirm
	setDCCMsg := &types.MsgDataCommitmentConfirm{
		Signature:        hex.EncodeToString(signature),
		ValidatorAddress: orchAddress.String(),
		EthAddress:       orchEthAddress,
		Commitment:       "commitment",
		BeginBlock:       1,
		EndBlock:         100,
	}
	result, err := h(ctx, setDCCMsg)
	require.NoError(t, err)

	// Checking if it was correctly submitted
	actualCommitment := k.GetDataCommitmentConfirm(ctx, "commitment", orchAddress)
	assert.Equal(t, setDCCMsg, actualCommitment)

	// Checking if the event was successfully sent
	actualEvent := result.Events[0]
	assert.Equal(t, sdk.EventTypeMessage, actualEvent.Type)
	assert.Equal(t, sdk.AttributeKeyModule, string(actualEvent.Attributes[0].Key))
	assert.Equal(t, setDCCMsg.Type(), string(actualEvent.Attributes[0].Value))
	assert.Equal(t, types.AttributeKeyDataCommitmentConfirmKey, string(actualEvent.Attributes[1].Key))
	assert.Equal(t, setDCCMsg.String(), string(actualEvent.Attributes[1].Value))
}
