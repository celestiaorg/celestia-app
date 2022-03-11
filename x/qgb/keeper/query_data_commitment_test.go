package keeper

import (
	"bytes"
	"fmt"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQueryDataCommitment(t *testing.T) {
	var (
		addrStr                       = "cosmos1v4s3yfg8rujaz56yt5a3xznqjqgyeff4552l40"
		myValidatorCosmosAddr, err1   = sdk.AccAddressFromBech32(addrStr)
		myValidatorEthereumAddr, err2 = types.NewEthAddress("0x3232323232323232323232323232323232323232")
	)
	require.NoError(t, err1)
	require.NoError(t, err2)
	input := CreateTestEnv(t)
	sdkCtx := input.Context
	ctx := sdk.WrapSDKContext(input.Context)
	k := input.QgbKeeper
	input.QgbKeeper.SetDataCommitmentConfirm(sdkCtx, types.MsgDataCommitmentConfirm{
		EthAddress:       myValidatorEthereumAddr.GetAddress(),
		Signature:        "alksdjhflkasjdfoiasjdfiasjdfoiasdj",
		ValidatorAddress: myValidatorCosmosAddr.String(),
		Commitment:       "commitment",
		BeginBlock:       10,
		EndBlock:         200,
	})

	specs := map[string]struct {
		src     types.QueryDataCommitmentConfirmRequest
		expErr  bool
		expResp types.QueryDataCommitmentConfirmResponse
	}{
		"all good": {
			src: types.QueryDataCommitmentConfirmRequest{
				Commitment: "commitment",
				Address:    myValidatorCosmosAddr.String(),
			},
			expResp: types.QueryDataCommitmentConfirmResponse{
				Confirm: types.NewMsgDataCommitmentConfirm(
					"commitment",
					"alksdjhflkasjdfoiasjdfiasjdfoiasdj",
					myValidatorCosmosAddr,
					*myValidatorEthereumAddr,
					10,
					200,
				),
			},
			expErr: false,
		},
		"unknown commitment": {
			src: types.QueryDataCommitmentConfirmRequest{
				Commitment: "wrong commitment",
				Address:    myValidatorCosmosAddr.String(),
			},
			expResp: types.QueryDataCommitmentConfirmResponse{Confirm: nil},
		},
		"invalid address": {
			src: types.QueryDataCommitmentConfirmRequest{
				Commitment: "commitment",
				Address:    "wrong address",
			},
			expErr: true,
		},
	}

	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			got, err := k.DataCommitmentConfirm(ctx, &spec.src)
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if spec.expResp == (types.QueryDataCommitmentConfirmResponse{}) {
				assert.True(t, got == nil || got.Confirm == nil)
				return
			}
			assert.Equal(t, &spec.expResp, got)
		})
	}
}

func TestAllDataCommitmentsByValidator(t *testing.T) {
	addr := "cosmos1v4s3yfg8rujaz56yt5a3xznqjqgyeff4552l40"
	commitments := []string{
		"commitment1",
		"commitment2",
		"commitment3",
	}
	var (
		myValidatorCosmosAddr1, _   = sdk.AccAddressFromBech32(addr)
		myValidatorEthereumAddr1, _ = types.NewEthAddress("0x0101010101010101010101010101010101010101")
	)

	input := CreateTestEnv(t)
	sdkCtx := input.Context
	ctx := sdk.WrapSDKContext(input.Context)
	k := input.QgbKeeper

	// seed commitments
	for i := 0; i < 3; i++ {
		addr, _ := sdk.AccAddressFromBech32(addr)
		msg := types.MsgDataCommitmentConfirm{}
		msg.EthAddress = myValidatorEthereumAddr1.GetAddress()
		msg.Commitment = commitments[i]
		msg.ValidatorAddress = addr.String()
		msg.Signature = fmt.Sprintf("signature %d", i+1)
		msg.BeginBlock = 1
		msg.EndBlock = 200
		input.QgbKeeper.SetDataCommitmentConfirm(sdkCtx, msg)
	}

	specs := map[string]struct {
		src     types.QueryDataCommitmentConfirmsByValidatorRequest
		expErr  bool
		expResp types.QueryDataCommitmentConfirmsByValidatorResponse
	}{
		"all good": {
			src: types.QueryDataCommitmentConfirmsByValidatorRequest{Address: addr},
			expResp: types.QueryDataCommitmentConfirmsByValidatorResponse{Confirms: []types.MsgDataCommitmentConfirm{
				*types.NewMsgDataCommitmentConfirm(
					commitments[0],
					"signature 1",
					myValidatorCosmosAddr1,
					*myValidatorEthereumAddr1,
					1,
					200,
				),
				*types.NewMsgDataCommitmentConfirm(
					commitments[1],
					"signature 2",
					myValidatorCosmosAddr1,
					*myValidatorEthereumAddr1,
					1,
					200,
				),
				*types.NewMsgDataCommitmentConfirm(
					commitments[2],
					"signature 3",
					myValidatorCosmosAddr1,
					*myValidatorEthereumAddr1,
					1,
					200,
				),
			}},
		},
		"unknown address": {
			src:     types.QueryDataCommitmentConfirmsByValidatorRequest{Address: "wrong address"},
			expResp: types.QueryDataCommitmentConfirmsByValidatorResponse{},
			expErr:  true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			got, err := k.DataCommitmentConfirmsByValidator(ctx, &spec.src)
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			var gotArray []types.MsgDataCommitmentConfirm
			if len(spec.expResp.Confirms) != 0 {
				gotArray = make([]types.MsgDataCommitmentConfirm, len(got.Confirms))
				copy(gotArray, got.Confirms)
			}
			assert.Equal(t, spec.expResp.Confirms, gotArray)
		})
	}
}

func TestAllDataCommitmentsByCommitment(t *testing.T) {
	commitment := "commitment"
	addrs := []string{
		"cosmos1v4s3yfg8rujaz56yt5a3xznqjqgyeff4552l40",
		"cosmos1dz6pu605p5x79dh5pz4dardhuzws6c0qqr0l6e",
		"cosmos1er9mgk7x30aspqd2zwn970ywfls36ktdmgyzry",
	}
	type blockRange struct {
		beingBlock int64
		endBlock   int64
	}
	ranges := []blockRange{
		{1, 101},
		{15, 120},
		{300, 450},
	}
	var (
		myValidatorCosmosAddr1, _   = sdk.AccAddressFromBech32(addrs[0])
		myValidatorCosmosAddr2, _   = sdk.AccAddressFromBech32(addrs[1])
		myValidatorCosmosAddr3, _   = sdk.AccAddressFromBech32(addrs[2])
		myValidatorEthereumAddr1, _ = types.NewEthAddress("0x0101010101010101010101010101010101010101")
		myValidatorEthereumAddr2, _ = types.NewEthAddress("0x0202020202020202020202020202020202020202")
		myValidatorEthereumAddr3, _ = types.NewEthAddress("0x0303030303030303030303030303030303030303")
	)

	input := CreateTestEnv(t)
	sdkCtx := input.Context
	ctx := sdk.WrapSDKContext(input.Context)
	k := input.QgbKeeper

	// seed confirmations
	for i := 0; i < 3; i++ {
		addr, _ := sdk.AccAddressFromBech32(addrs[i])
		msg := types.MsgDataCommitmentConfirm{}
		msg.EthAddress = gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(i + 1)}, 20)).String()
		msg.Commitment = commitment
		msg.BeginBlock = ranges[i].beingBlock
		msg.EndBlock = ranges[i].endBlock
		msg.ValidatorAddress = addr.String()
		msg.Signature = fmt.Sprintf("signature %d", i+1)
		input.QgbKeeper.SetDataCommitmentConfirm(sdkCtx, msg)
	}

	specs := map[string]struct {
		src     types.QueryDataCommitmentConfirmsByRangeRequest
		expErr  bool
		expResp types.QueryDataCommitmentConfirmsByRangeResponse
	}{
		"all range": {
			src: types.QueryDataCommitmentConfirmsByRangeRequest{BeginBlock: 1, EndBlock: 500},
			expResp: types.QueryDataCommitmentConfirmsByRangeResponse{Confirms: []types.MsgDataCommitmentConfirm{
				*types.NewMsgDataCommitmentConfirm(
					commitment,
					"signature 1",
					myValidatorCosmosAddr1,
					*myValidatorEthereumAddr1,
					ranges[0].beingBlock,
					ranges[0].endBlock,
				),
				*types.NewMsgDataCommitmentConfirm(
					commitment,
					"signature 2",
					myValidatorCosmosAddr2,
					*myValidatorEthereumAddr2,
					ranges[1].beingBlock,
					ranges[1].endBlock,
				),
				*types.NewMsgDataCommitmentConfirm(
					commitment,
					"signature 3",
					myValidatorCosmosAddr3,
					*myValidatorEthereumAddr3,
					ranges[2].beingBlock,
					ranges[2].endBlock,
				),
			}},
		},
		"partial range 1 - 200": {
			src: types.QueryDataCommitmentConfirmsByRangeRequest{BeginBlock: 1, EndBlock: 200},
			expResp: types.QueryDataCommitmentConfirmsByRangeResponse{Confirms: []types.MsgDataCommitmentConfirm{
				*types.NewMsgDataCommitmentConfirm(
					commitment,
					"signature 1",
					myValidatorCosmosAddr1,
					*myValidatorEthereumAddr1,
					ranges[0].beingBlock,
					ranges[0].endBlock,
				),
				*types.NewMsgDataCommitmentConfirm(
					commitment,
					"signature 2",
					myValidatorCosmosAddr2,
					*myValidatorEthereumAddr2,
					ranges[1].beingBlock,
					ranges[1].endBlock,
				),
			}},
		},
		"partial range 201 - 500": {
			src: types.QueryDataCommitmentConfirmsByRangeRequest{BeginBlock: 201, EndBlock: 500},
			expResp: types.QueryDataCommitmentConfirmsByRangeResponse{Confirms: []types.MsgDataCommitmentConfirm{
				*types.NewMsgDataCommitmentConfirm(
					commitment,
					"signature 3",
					myValidatorCosmosAddr3,
					*myValidatorEthereumAddr3,
					ranges[2].beingBlock,
					ranges[2].endBlock,
				),
			}},
		},
		"empty range": {
			src:     types.QueryDataCommitmentConfirmsByRangeRequest{BeginBlock: 800, EndBlock: 1000},
			expResp: types.QueryDataCommitmentConfirmsByRangeResponse{},
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			got, err := k.DataCommitmentConfirmsByRange(ctx, &spec.src)
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			var gotArray []types.MsgDataCommitmentConfirm
			if len(spec.expResp.Confirms) != 0 {
				gotArray = make([]types.MsgDataCommitmentConfirm, len(got.Confirms))
				copy(gotArray, got.Confirms)
			}
			assert.Equal(t, spec.expResp.Confirms, gotArray)
		})
	}
}
