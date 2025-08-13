package ante_test

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v5/app"
	"github.com/celestiaorg/celestia-app/v5/app/ante"
	"github.com/celestiaorg/celestia-app/v5/app/encoding"
	"github.com/celestiaorg/celestia-app/v5/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v5/test/util"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestDisableVestingDecorator(t *testing.T) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	decorator := ante.NewDisableVestingDecorator()

	// Helper to create vesting message
	createVestingMsg := func(fromIdx, toIdx int) *vestingtypes.MsgCreateVestingAccount {
		return &vestingtypes.MsgCreateVestingAccount{
			FromAddress: testutil.AccPubKeys[fromIdx].Address().String(),
			ToAddress:   testutil.AccPubKeys[toIdx].Address().String(),
			Amount:      sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1000))),
			Delayed:     true,
			EndTime:     time.Now().Add(2 * time.Hour).Unix(),
			StartTime:   time.Now().Add(1 * time.Hour).Unix(),
		}
	}

	// Helper to create bank send message
	createSendMsg := func(fromIdx, toIdx int) *banktypes.MsgSend {
		return &banktypes.MsgSend{
			FromAddress: testutil.AccPubKeys[fromIdx].Address().String(),
			ToAddress:   testutil.AccPubKeys[toIdx].Address().String(),
			Amount:      sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(500))),
		}
	}

	tests := []struct {
		name      string
		messages  []sdk.Msg
		signMode  signing.SignMode
		simulate  bool
		checkTx   bool
		expectErr bool
		errText   string
	}{
		{
			name:      "non-vesting message with amino JSON",
			messages:  []sdk.Msg{createSendMsg(0, 1)},
			signMode:  signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
			simulate:  false,
			checkTx:   true,
			expectErr: false,
		},
		{
			name:      "vesting message with direct sign mode signing",
			messages:  []sdk.Msg{createVestingMsg(0, 1)},
			signMode:  signing.SignMode_SIGN_MODE_DIRECT,
			simulate:  false,
			checkTx:   true,
			expectErr: false,
		},
		{
			name:      "vesting message with amino JSON signing",
			messages:  []sdk.Msg{createVestingMsg(0, 1)},
			signMode:  signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
			simulate:  false,
			checkTx:   true,
			expectErr: true,
			errText:   "MsgCreateVestingAccount is temporarily disabled with amino JSON signing",
		},
		{
			name:      "multiple vesting messages with amino JSON",
			messages:  []sdk.Msg{createVestingMsg(0, 1), createVestingMsg(0, 2)},
			signMode:  signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
			simulate:  false,
			checkTx:   true,
			expectErr: true,
			errText:   "MsgCreateVestingAccount is temporarily disabled with amino JSON signing",
		},
		{
			name:      "mixed messages (vesting + send) with protobuf",
			messages:  []sdk.Msg{createVestingMsg(0, 1), createSendMsg(0, 2)},
			signMode:  signing.SignMode_SIGN_MODE_DIRECT,
			simulate:  false,
			checkTx:   true,
			expectErr: false,
		},
		{
			name:      "mixed messages (vesting + send) with amino JSON",
			messages:  []sdk.Msg{createVestingMsg(0, 1), createSendMsg(0, 2)},
			signMode:  signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
			simulate:  false,
			checkTx:   true,
			expectErr: true,
			errText:   "MsgCreateVestingAccount is temporarily disabled with amino JSON signing",
		},
		{
			name:      "mixed messages (send + vesting) with amino JSON",
			messages:  []sdk.Msg{createSendMsg(0, 2), createVestingMsg(0, 1)},
			signMode:  signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
			simulate:  false,
			checkTx:   true,
			expectErr: true,
			errText:   "MsgCreateVestingAccount is temporarily disabled with amino JSON signing",
		},
		{
			name:      "vesting with amino JSON in simulation mode",
			messages:  []sdk.Msg{createVestingMsg(0, 1)},
			signMode:  signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
			simulate:  true,
			checkTx:   true,
			expectErr: true,
			errText:   "MsgCreateVestingAccount is temporarily disabled with amino JSON signing",
		},
		{
			name:      "vesting with amino JSON - checkTx false (decorator bypassed)",
			messages:  []sdk.Msg{createVestingMsg(0, 1)},
			signMode:  signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
			simulate:  false,
			checkTx:   false,
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create context with the specified checkTx parameter
			ctx := testApp.NewContext(tt.checkTx)

			enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
			builder := enc.TxConfig.NewTxBuilder()
			err := builder.SetMsgs(tt.messages...)
			require.NoError(t, err)

			// Set signature with the specified sign mode
			sig := signing.SignatureV2{
				PubKey: testutil.AccPubKeys[0],
				Data: &signing.SingleSignatureData{
					SignMode:  tt.signMode,
					Signature: []byte("mock_signature"),
				},
				Sequence: 0,
			}
			err = builder.SetSignatures(sig)
			require.NoError(t, err)

			tx := builder.GetTx()

			// Test the decorator
			_, err = decorator.AnteHandle(ctx, tx, tt.simulate, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				return ctx, nil
			})

			if tt.expectErr {
				require.Error(t, err)
				if tt.errText != "" {
					require.Contains(t, err.Error(), tt.errText)
					require.True(t, sdkerrors.ErrUnauthorized.Is(err))
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
