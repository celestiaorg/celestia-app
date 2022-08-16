package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryDataCommitment(t *testing.T) {
	var (
		addrStr                     = "cosmos1v4s3yfg8rujaz56yt5a3xznqjqgyeff4552l40"
		myValidatorCosmosAddr, err1 = sdk.AccAddressFromBech32(addrStr)
		myValidatorEthereumAddr     = gethcommon.HexToAddress("0x3232323232323232323232323232323232323232")
		nonce                       = uint64(20)
	)
	require.NoError(t, err1)
	input := testutil.CreateTestEnv(t)
	sdkCtx := input.Context
	ctx := sdk.WrapSDKContext(input.Context)
	k := input.QgbKeeper
	_, _ = input.QgbKeeper.SetDataCommitmentConfirm(
		sdkCtx,
		*types.NewMsgDataCommitmentConfirm(
			"commitment",
			"alksdjhflkasjdfoiasjdfiasjdfoiasdj",
			myValidatorCosmosAddr,
			myValidatorEthereumAddr,
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
					myValidatorEthereumAddr,
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
