package keeper_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryDataCommitment(t *testing.T) {
	var (
		addrStr                       = "cosmos1v4s3yfg8rujaz56yt5a3xznqjqgyeff4552l40"
		myValidatorCosmosAddr, err1   = sdk.AccAddressFromBech32(addrStr)
		myValidatorEthereumAddr, err2 = stakingtypes.NewEthAddress("0x3232323232323232323232323232323232323232")
		nonce                         = uint64(20)
	)
	require.NoError(t, err1)
	require.NoError(t, err2)
	input := testutil.CreateTestEnv(t)
	sdkCtx := input.Context
	ctx := sdk.WrapSDKContext(input.Context)
	k := input.QgbKeeper
	input.QgbKeeper.SetDataCommitmentConfirm(
		sdkCtx,
		*types.NewMsgDataCommitmentConfirm(
			"commitment",
			"alksdjhflkasjdfoiasjdfiasjdfoiasdj",
			myValidatorCosmosAddr,
			*myValidatorEthereumAddr,
			10,
			200,
			nonce,
		),
	)

	specs := map[string]struct {
		src     types.QueryDataCommitmentConfirmRequest
		expErr  bool
		expResp types.QueryDataCommitmentConfirmResponse
	}{
		"all good": {
			src: types.QueryDataCommitmentConfirmRequest{
				BeginBlock: 10,
				EndBlock:   200,
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
					nonce,
				),
			},
			expErr: false,
		},
		"unknown end block": {
			src: types.QueryDataCommitmentConfirmRequest{
				BeginBlock: 10,
				EndBlock:   199,
				Address:    myValidatorCosmosAddr.String(),
			},
			expResp: types.QueryDataCommitmentConfirmResponse{Confirm: nil},
		},
		"unknown begin block": {
			src: types.QueryDataCommitmentConfirmRequest{
				BeginBlock: 11,
				EndBlock:   200,
				Address:    myValidatorCosmosAddr.String(),
			},
			expResp: types.QueryDataCommitmentConfirmResponse{Confirm: nil},
		},
		"invalid address": {
			src: types.QueryDataCommitmentConfirmRequest{
				BeginBlock: 10,
				EndBlock:   200,
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

func TestAllDataCommitmentsByCommitment(t *testing.T) {
	type blockRange struct {
		beginBlock uint64
		endBlock   uint64
	}
	var (
		commitment       = "commitment"
		secondCommitment = "second commitment"
		addrs            = []string{
			"cosmos1v4s3yfg8rujaz56yt5a3xznqjqgyeff4552l40",
			"cosmos1dz6pu605p5x79dh5pz4dardhuzws6c0qqr0l6e",
			"cosmos1er9mgk7x30aspqd2zwn970ywfls36ktdmgyzry",
		}
		ranges = []blockRange{
			{1, 101},
			{15, 120},
			{300, 450},
		}
		myValidatorCosmosAddr1, _   = sdk.AccAddressFromBech32(addrs[0])
		myValidatorCosmosAddr2, _   = sdk.AccAddressFromBech32(addrs[1])
		myValidatorCosmosAddr3, _   = sdk.AccAddressFromBech32(addrs[2])
		myValidatorEthereumAddr1, _ = stakingtypes.NewEthAddress("0x0101010101010101010101010101010101010101")
		myValidatorEthereumAddr2, _ = stakingtypes.NewEthAddress("0x0202020202020202020202020202020202020202")
		myValidatorEthereumAddr3, _ = stakingtypes.NewEthAddress("0x0303030303030303030303030303030303030303")
		nonce                       = uint64(20)
	)

	input := testutil.CreateTestEnv(t)
	sdkCtx := input.Context
	ctx := sdk.WrapSDKContext(input.Context)
	k := input.QgbKeeper

	// seed confirmations
	for i := 0; i < 3; i++ {
		addr, _ := sdk.AccAddressFromBech32(addrs[i])
		ethAddr, _ := stakingtypes.NewEthAddress(gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(i + 1)}, 20)).String())
		input.QgbKeeper.SetDataCommitmentConfirm(
			sdkCtx,
			*types.NewMsgDataCommitmentConfirm(
				commitment,
				fmt.Sprintf("signature %d", i+1),
				addr,
				*ethAddr,
				ranges[i].beginBlock,
				ranges[i].endBlock,
				nonce,
			),
		)
	}

	// seed a second commitment message
	addr, _ := sdk.AccAddressFromBech32(addrs[0])
	ethAddr, _ := stakingtypes.NewEthAddress(gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(1)}, 20)).String())

	input.QgbKeeper.SetDataCommitmentConfirm(
		sdkCtx,
		*types.NewMsgDataCommitmentConfirm(
			secondCommitment,
			"signature 1",
			addr,
			*ethAddr,
			800,
			900,
			nonce,
		),
	)

	specs := map[string]struct {
		src     types.QueryDataCommitmentConfirmsByCommitmentRequest
		expErr  bool
		expResp types.QueryDataCommitmentConfirmsByCommitmentResponse
	}{
		"existing commitment": {
			src: types.QueryDataCommitmentConfirmsByCommitmentRequest{Commitment: commitment},
			expResp: types.QueryDataCommitmentConfirmsByCommitmentResponse{Confirms: []types.MsgDataCommitmentConfirm{
				*types.NewMsgDataCommitmentConfirm(
					commitment,
					"signature 1",
					myValidatorCosmosAddr1,
					*myValidatorEthereumAddr1,
					ranges[0].beginBlock,
					ranges[0].endBlock,
					nonce,
				),
				*types.NewMsgDataCommitmentConfirm(
					commitment,
					"signature 2",
					myValidatorCosmosAddr2,
					*myValidatorEthereumAddr2,
					ranges[1].beginBlock,
					ranges[1].endBlock,
					nonce,
				),
				*types.NewMsgDataCommitmentConfirm(
					commitment,
					"signature 3",
					myValidatorCosmosAddr3,
					*myValidatorEthereumAddr3,
					ranges[2].beginBlock,
					ranges[2].endBlock,
					nonce,
				),
			}},
		},
		"unknown commitment": {
			src:     types.QueryDataCommitmentConfirmsByCommitmentRequest{Commitment: "unknown commitment"},
			expResp: types.QueryDataCommitmentConfirmsByCommitmentResponse{},
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			got, err := k.DataCommitmentConfirmsByCommitment(ctx, &spec.src)
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, len(got.Confirms), len(spec.expResp.Confirms))
			for i := 0; i < len(spec.expResp.Confirms); i++ {
				assert.Contains(t, spec.expResp.Confirms, got.Confirms[i])
			}
		})
	}
}
