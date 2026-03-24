package keeper_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/keeper"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// MockBankKeeper implements types.BankKeeper for testing IGP fee flows
type MockBankKeeper struct {
	Balances     map[string]sdk.Coins // addr -> coins
	SendCoinsErr error                // inject errors on SendCoins
	SendCoinsFn  func(ctx context.Context, from, to sdk.AccAddress, amt sdk.Coins) error
}

func NewMockBankKeeper() *MockBankKeeper {
	return &MockBankKeeper{
		Balances: make(map[string]sdk.Coins),
	}
}

func (m *MockBankKeeper) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	for _, c := range m.Balances[addr.String()] {
		if c.Denom == denom {
			return c
		}
	}
	return sdk.NewCoin(denom, math.ZeroInt())
}

func (m *MockBankKeeper) SendCoins(ctx context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	if m.SendCoinsFn != nil {
		return m.SendCoinsFn(ctx, from, to, amt)
	}
	if m.SendCoinsErr != nil {
		return m.SendCoinsErr
	}
	// Update balances
	fromBal := m.Balances[from.String()]
	toBal := m.Balances[to.String()]

	newFromBal, hasNeg := fromBal.SafeSub(amt...)
	if hasNeg {
		return errors.New("insufficient funds")
	}

	m.Balances[from.String()] = newFromBal
	m.Balances[to.String()] = toBal.Add(amt...)
	return nil
}

// MockWarpKeeper implements types.WarpKeeper for testing
type MockWarpKeeper struct {
	Tokens            []warptypes.HypToken
	EnrolledRouters   map[uint64]map[uint32]warptypes.RemoteRouter // tokenId -> domain -> router
	TransferErr       error
	TransferMessageId util.HexAddress
	// OnTransfer is called during transfer to simulate side effects (like IGP consumption)
	// Parameters: sender address, maxFee provided
	OnTransfer func(sender string, maxFee sdk.Coin)
}

func NewMockWarpKeeper() *MockWarpKeeper {
	return &MockWarpKeeper{
		Tokens:          make([]warptypes.HypToken, 0),
		EnrolledRouters: make(map[uint64]map[uint32]warptypes.RemoteRouter),
	}
}

func (m *MockWarpKeeper) RemoteTransferSynthetic(
	_ sdk.Context,
	_ warptypes.HypToken,
	sender string,
	_ uint32,
	_ util.HexAddress,
	_ math.Int,
	_ *util.HexAddress,
	_ math.Int,
	maxFee sdk.Coin,
	_ []byte,
) (util.HexAddress, error) {
	if m.TransferErr != nil {
		return util.HexAddress{}, m.TransferErr
	}
	if m.OnTransfer != nil {
		m.OnTransfer(sender, maxFee)
	}
	return m.TransferMessageId, nil
}

func (m *MockWarpKeeper) RemoteTransferCollateral(
	_ sdk.Context,
	_ warptypes.HypToken,
	sender string,
	_ uint32,
	_ util.HexAddress,
	_ math.Int,
	_ *util.HexAddress,
	_ math.Int,
	maxFee sdk.Coin,
	_ []byte,
) (util.HexAddress, error) {
	if m.TransferErr != nil {
		return util.HexAddress{}, m.TransferErr
	}
	if m.OnTransfer != nil {
		m.OnTransfer(sender, maxFee)
	}
	return m.TransferMessageId, nil
}

func (m *MockWarpKeeper) GetHypToken(_ context.Context, id uint64) (warptypes.HypToken, error) {
	for _, t := range m.Tokens {
		if t.Id.GetInternalId() == id {
			return t, nil
		}
	}
	return warptypes.HypToken{}, errors.New("token not found")
}

func (m *MockWarpKeeper) HasEnrolledRouter(_ context.Context, tokenId uint64, domain uint32) (bool, error) {
	if routes, ok := m.EnrolledRouters[tokenId]; ok {
		_, hasRoute := routes[domain]
		return hasRoute, nil
	}
	return false, nil
}

func (m *MockWarpKeeper) GetEnrolledRouter(_ context.Context, tokenId uint64, domain uint32) (warptypes.RemoteRouter, error) {
	if routes, ok := m.EnrolledRouters[tokenId]; ok {
		if router, hasRoute := routes[domain]; hasRoute {
			return router, nil
		}
	}
	return warptypes.RemoteRouter{}, errors.New("no router enrolled")
}

// MockHyperlaneKeeper implements types.HyperlaneKeeper for testing
type MockHyperlaneKeeper struct {
	QuotedFee sdk.Coins
	QuoteErr  error
}

func NewMockHyperlaneKeeper() *MockHyperlaneKeeper {
	return &MockHyperlaneKeeper{}
}

func (m *MockHyperlaneKeeper) QuoteDispatch(
	_ context.Context,
	_ util.HexAddress,
	_ util.HexAddress,
	_ util.StandardHookMetadata,
	_ util.HyperlaneMessage,
) (sdk.Coins, error) {
	if m.QuoteErr != nil {
		return nil, m.QuoteErr
	}
	return m.QuotedFee, nil
}

// Test helpers
func createTestContext() sdk.Context {
	return testutil.DefaultContext(storetypes.NewKVStoreKey("testkv"), storetypes.NewTransientStoreKey("testtransient"))
}

// deriveTestForwardAddress derives a forwarding address from the given destDomain, destRecipient, and token.
func deriveTestForwardAddress(destDomain uint32, destRecipientHex, tokenID string) (sdk.AccAddress, error) {
	destRecipient, err := util.DecodeHexAddress(destRecipientHex)
	if err != nil {
		return nil, err
	}
	token, err := util.DecodeHexAddress(tokenID)
	if err != nil {
		return nil, err
	}
	addrBytes, err := types.DeriveForwardingAddress(destDomain, destRecipient.Bytes(), token.Bytes())
	if err != nil {
		return nil, err
	}
	return sdk.AccAddress(addrBytes), nil
}

func createTestHypToken(id uint64, denom string, tokenType warptypes.HypTokenType) warptypes.HypToken {
	hexId, _ := util.DecodeHexAddress("0x" + padHex(id))
	return warptypes.HypToken{
		Id:          hexId,
		OriginDenom: denom,
		TokenType:   tokenType,
	}
}

func padHex(id uint64) string {
	return fmt.Sprintf("%064x", id)
}

// testIGPSetup holds common test fixtures for IGP fee tests
type testIGPSetup struct {
	ctx             sdk.Context
	destDomain      uint32
	destRecipient   string
	tokenID         string
	token           warptypes.HypToken
	forwardAddr     sdk.AccAddress
	signer          sdk.AccAddress
	bankKeeper      *MockBankKeeper
	warpKeeper      *MockWarpKeeper
	hyperlaneKeeper *MockHyperlaneKeeper
	msgServer       types.MsgServer
}

func newTestIGPSetup(t *testing.T) *testIGPSetup {
	t.Helper()
	ctx := createTestContext()
	destDomain := uint32(42161)
	destRecipient := "0x00000000000000000000000000000000000000000000000000000000deadbeef"

	signer := sdk.AccAddress([]byte("signer______________"))
	bankKeeper := NewMockBankKeeper()
	warpKeeper := NewMockWarpKeeper()
	hyperlaneKeeper := NewMockHyperlaneKeeper()

	// Setup TIA collateral token with route
	tiaToken := createTestHypToken(1, appconsts.BondDenom, warptypes.HYP_TOKEN_TYPE_COLLATERAL)
	forwardAddr, err := deriveTestForwardAddress(destDomain, destRecipient, tiaToken.Id.String())
	require.NoError(t, err)
	warpKeeper.Tokens = append(warpKeeper.Tokens, tiaToken)
	warpKeeper.EnrolledRouters[1] = map[uint32]warptypes.RemoteRouter{
		destDomain: {Gas: math.NewInt(200000)},
	}

	k := keeper.NewKeeper(bankKeeper, warpKeeper, hyperlaneKeeper)

	return &testIGPSetup{
		ctx:             ctx,
		destDomain:      destDomain,
		destRecipient:   destRecipient,
		tokenID:         tiaToken.Id.String(),
		token:           tiaToken,
		forwardAddr:     forwardAddr,
		signer:          signer,
		bankKeeper:      bankKeeper,
		warpKeeper:      warpKeeper,
		hyperlaneKeeper: hyperlaneKeeper,
		msgServer:       keeper.NewMsgServerImpl(k),
	}
}

// TestForwardSingleToken_IGPFeeValidation tests pre-warp IGP fee validation failures
func TestForwardSingleToken_IGPFeeValidation(t *testing.T) {
	testCases := []struct {
		name              string
		quotedFee         sdk.Coins
		maxIgpFee         sdk.Coin
		signerBalance     sdk.Coins
		expectedSignerBal math.Int
		expectedErrPart   string
	}{
		{
			name:              "insufficient max_igp_fee",
			quotedFee:         sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(100))),
			maxIgpFee:         sdk.NewCoin(appconsts.BondDenom, math.NewInt(50)), // 50 < 100
			signerBalance:     sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(200))),
			expectedSignerBal: math.NewInt(200),
			expectedErrPart:   appconsts.BondDenom + ":1000",
		},
		{
			name:              "denom mismatch",
			quotedFee:         sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(100))),
			maxIgpFee:         sdk.NewCoin("uother", math.NewInt(100)), // wrong denom
			signerBalance:     sdk.NewCoins(sdk.NewCoin("uother", math.NewInt(200))),
			expectedSignerBal: math.NewInt(200),
			expectedErrPart:   "max_igp_fee denom mismatch",
		},
		{
			name:              "signer insufficient balance",
			quotedFee:         sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(100))),
			maxIgpFee:         sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
			signerBalance:     sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(50))), // 50 < 100
			expectedSignerBal: math.NewInt(50),
			expectedErrPart:   "failed to collect IGP fee from relayer: insufficient funds",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestIGPSetup(t)

			// Setup balances
			s.bankKeeper.Balances[s.forwardAddr.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
			s.bankKeeper.Balances[s.signer.String()] = tc.signerBalance
			s.hyperlaneKeeper.QuotedFee = tc.quotedFee

			msg := types.NewMsgForward(
				s.signer.String(),
				s.forwardAddr.String(),
				s.destDomain,
				s.destRecipient,
				s.tokenID,
				tc.maxIgpFee,
			)

			resp, err := s.msgServer.Forward(s.ctx, msg)

			require.Error(t, err)
			require.Nil(t, resp)
			require.ErrorIs(t, err, types.ErrForwardFailed)
			require.ErrorContains(t, err, tc.expectedErrPart)

			// Verify no balance changes (validation failed before transfers)
			require.Equal(t, math.NewInt(1000), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount)
			require.Equal(t, tc.expectedSignerBal, s.bankKeeper.GetBalance(s.ctx, s.signer, tc.maxIgpFee.Denom).Amount)
		})
	}
}

// TestForwardSingleToken_IGPFeeRefundOnSuccess tests that excess IGP fee is refunded to signer
func TestForwardSingleToken_IGPFeeRefundOnSuccess(t *testing.T) {
	s := newTestIGPSetup(t)

	// Setup balances
	s.bankKeeper.Balances[s.forwardAddr.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	s.bankKeeper.Balances[s.signer.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(200)))
	s.hyperlaneKeeper.QuotedFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)))

	// Warp succeeds
	messageId, _ := util.DecodeHexAddress("0x0000000000000000000000000000000000000000000000000000000000001234")
	s.warpKeeper.TransferMessageId = messageId

	// Simulate a same-denom warp transfer consuming the forwarded 1000 utia plus
	// only 80 utia of the quoted IGP fee.
	forwardedAmount := math.NewInt(1000)
	actualIgpConsumed := math.NewInt(80)
	s.warpKeeper.OnTransfer = func(sender string, maxFee sdk.Coin) {
		senderAddr, _ := sdk.AccAddressFromBech32(sender)
		currentBal := s.bankKeeper.Balances[senderAddr.String()]
		consumed := sdk.NewCoins(sdk.NewCoin(maxFee.Denom, forwardedAmount.Add(actualIgpConsumed)))
		s.bankKeeper.Balances[senderAddr.String()] = currentBal.Sub(consumed...)
	}

	msg := types.NewMsgForward(
		s.signer.String(),
		s.forwardAddr.String(),
		s.destDomain,
		s.destRecipient,
		s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)

	resp, err := s.msgServer.Forward(s.ctx, msg)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, appconsts.BondDenom, resp.Denom)
	require.Equal(t, math.NewInt(1000), resp.Amount)
	require.Equal(t, messageId.String(), resp.MessageId)

	// Verify: signer paid 100, got 20 refund, net cost = 80
	// Final signer balance = 200 - 100 + 20 = 120
	require.Equal(t, math.NewInt(120), s.bankKeeper.GetBalance(s.ctx, s.signer, appconsts.BondDenom).Amount,
		"signer should have received refund of excess IGP fee")
}

func TestForwardSingleToken_IGPFeeRefundFailureReturnsError(t *testing.T) {
	s := newTestIGPSetup(t)

	s.bankKeeper.Balances[s.forwardAddr.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	s.bankKeeper.Balances[s.signer.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(200)))
	s.hyperlaneKeeper.QuotedFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)))

	messageId, _ := util.DecodeHexAddress("0x0000000000000000000000000000000000000000000000000000000000001234")
	s.warpKeeper.TransferMessageId = messageId

	forwardedAmount := math.NewInt(1000)
	actualIgpConsumed := math.NewInt(80)
	s.warpKeeper.OnTransfer = func(sender string, maxFee sdk.Coin) {
		senderAddr, _ := sdk.AccAddressFromBech32(sender)
		currentBal := s.bankKeeper.Balances[senderAddr.String()]
		consumed := sdk.NewCoins(sdk.NewCoin(maxFee.Denom, forwardedAmount.Add(actualIgpConsumed)))
		s.bankKeeper.Balances[senderAddr.String()] = currentBal.Sub(consumed...)
	}

	originalSendCoinsFn := s.bankKeeper.SendCoinsFn
	s.bankKeeper.SendCoinsFn = func(ctx context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
		if from.Equals(s.forwardAddr) && to.Equals(s.signer) {
			return errors.New("refund send failed")
		}
		if originalSendCoinsFn != nil {
			return originalSendCoinsFn(ctx, from, to, amt)
		}

		fromBal := s.bankKeeper.Balances[from.String()]
		toBal := s.bankKeeper.Balances[to.String()]

		newFromBal, hasNeg := fromBal.SafeSub(amt...)
		if hasNeg {
			return errors.New("insufficient funds")
		}

		s.bankKeeper.Balances[from.String()] = newFromBal
		s.bankKeeper.Balances[to.String()] = toBal.Add(amt...)
		return nil
	}

	msg := types.NewMsgForward(
		s.signer.String(),
		s.forwardAddr.String(),
		s.destDomain,
		s.destRecipient,
		s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)

	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.Error(t, err)
	require.Nil(t, resp)
	require.ErrorIs(t, err, types.ErrForwardFailed)
	require.ErrorContains(t, err, "failed to refund excess IGP fee to relayer")
}

func TestForward_LeavesUnrelatedBalancesUntouched(t *testing.T) {
	s := newTestIGPSetup(t)
	s.bankKeeper.Balances[s.forwardAddr.String()] = sdk.NewCoins(
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(1_000_000)),
		sdk.NewCoin("ibc/unrelated", math.NewInt(42)),
	)
	s.bankKeeper.Balances[s.signer.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)))
	s.hyperlaneKeeper.QuotedFee = sdk.NewCoins()

	messageId, _ := util.DecodeHexAddress("0x0000000000000000000000000000000000000000000000000000000000001234")
	s.warpKeeper.TransferMessageId = messageId
	s.warpKeeper.OnTransfer = func(sender string, _ sdk.Coin) {
		senderAddr, _ := sdk.AccAddressFromBech32(sender)
		s.bankKeeper.Balances[senderAddr.String()] = sdk.NewCoins(sdk.NewCoin("ibc/unrelated", math.NewInt(42)))
	}

	msg := types.NewMsgForward(
		s.signer.String(),
		s.forwardAddr.String(),
		s.destDomain,
		s.destRecipient,
		s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
	)

	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, appconsts.BondDenom, resp.Denom)
	require.Equal(t, math.NewInt(1_000_000), resp.Amount)
	require.Equal(t, messageId.String(), resp.MessageId)
	require.True(t, s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).IsZero())
	require.Equal(t, math.NewInt(42), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, "ibc/unrelated").Amount)
}

func TestForward_NoBalanceForBoundToken(t *testing.T) {
	s := newTestIGPSetup(t)
	s.bankKeeper.Balances[s.forwardAddr.String()] = sdk.NewCoins(sdk.NewCoin("ibc/unrelated", math.NewInt(25)))

	msg := types.NewMsgForward(
		s.signer.String(),
		s.forwardAddr.String(),
		s.destDomain,
		s.destRecipient,
		s.tokenID,
		sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
	)

	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.ErrorIs(t, err, types.ErrNoBalance)
	require.Nil(t, resp)
	require.Equal(t, math.NewInt(25), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, "ibc/unrelated").Amount)
}

func TestForward_AddressMismatchWhenTokenIDChanges(t *testing.T) {
	s := newTestIGPSetup(t)

	otherToken := createTestHypToken(2, appconsts.BondDenom, warptypes.HYP_TOKEN_TYPE_COLLATERAL)
	s.warpKeeper.Tokens = append(s.warpKeeper.Tokens, otherToken)
	s.warpKeeper.EnrolledRouters[2] = map[uint32]warptypes.RemoteRouter{
		s.destDomain: {Gas: math.NewInt(200000)},
	}

	msg := types.NewMsgForward(
		s.signer.String(),
		s.forwardAddr.String(),
		s.destDomain,
		s.destRecipient,
		otherToken.Id.String(),
		sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
	)

	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.Error(t, err)
	require.Nil(t, resp)
	require.ErrorIs(t, err, types.ErrAddressMismatch)
}

func TestForward_SyntheticToken(t *testing.T) {
	ctx := createTestContext()
	destDomain := uint32(999)
	destRecipient := "0x00000000000000000000000000000000000000000000000000000000cafebabe"
	signer := sdk.AccAddress([]byte("signer______________"))
	bankKeeper := NewMockBankKeeper()
	warpKeeper := NewMockWarpKeeper()
	hyperlaneKeeper := NewMockHyperlaneKeeper()

	synthToken := createTestHypToken(2, "uusdc", warptypes.HYP_TOKEN_TYPE_SYNTHETIC)
	forwardAddr, err := deriveTestForwardAddress(destDomain, destRecipient, synthToken.Id.String())
	require.NoError(t, err)

	warpKeeper.Tokens = append(warpKeeper.Tokens, synthToken)
	warpKeeper.EnrolledRouters[2] = map[uint32]warptypes.RemoteRouter{
		destDomain: {Gas: math.NewInt(250000)},
	}
	hyperlaneKeeper.QuotedFee = sdk.NewCoins()

	messageId, _ := util.DecodeHexAddress("0x0000000000000000000000000000000000000000000000000000000000001234")
	warpKeeper.TransferMessageId = messageId
	synthDenom := "hyperlane/" + synthToken.Id.String()
	warpKeeper.OnTransfer = func(sender string, _ sdk.Coin) {
		senderAddr, _ := sdk.AccAddressFromBech32(sender)
		senderBalances := bankKeeper.Balances[senderAddr.String()]
		bankKeeper.Balances[senderAddr.String()] = senderBalances.Sub(sdk.NewCoin(synthDenom, math.NewInt(75)))
	}

	bankKeeper.Balances[forwardAddr.String()] = sdk.NewCoins(
		sdk.NewCoin(synthDenom, math.NewInt(75)),
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(11)),
	)

	k := keeper.NewKeeper(bankKeeper, warpKeeper, hyperlaneKeeper)
	msgServer := keeper.NewMsgServerImpl(k)

	msg := types.NewMsgForward(
		signer.String(),
		forwardAddr.String(),
		destDomain,
		destRecipient,
		synthToken.Id.String(),
		sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()),
	)

	resp, err := msgServer.Forward(ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, synthDenom, resp.Denom)
	require.Equal(t, math.NewInt(75), resp.Amount)
	require.Equal(t, messageId.String(), resp.MessageId)
	require.True(t, bankKeeper.GetBalance(ctx, forwardAddr, synthDenom).IsZero())
	require.Equal(t, math.NewInt(11), bankKeeper.GetBalance(ctx, forwardAddr, appconsts.BondDenom).Amount)
}
