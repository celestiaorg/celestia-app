package keeper_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/keeper"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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

func (m *MockBankKeeper) GetAllBalances(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.Balances[addr.String()]
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

func (m *MockWarpKeeper) GetAllHypTokens(_ context.Context) ([]warptypes.HypToken, error) {
	return m.Tokens, nil
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
	return sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger()).WithGasMeter(storetypes.NewInfiniteGasMeter())
}

// deriveTestForwardAddress derives a forwarding address from the given destDomain and destRecipient
func deriveTestForwardAddress(destDomain uint32, destRecipientHex string) (sdk.AccAddress, error) {
	destRecipient, err := util.DecodeHexAddress(destRecipientHex)
	if err != nil {
		return nil, err
	}
	addrBytes, err := types.DeriveForwardingAddress(destDomain, destRecipient.Bytes())
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
	hex := "0000000000000000000000000000000000000000000000000000000000000000"
	idStr := "1"
	if id > 0 {
		idStr = string(rune('0' + id))
	}
	return hex[:len(hex)-len(idStr)] + idStr
}

// testIGPSetup holds common test fixtures for IGP fee tests
type testIGPSetup struct {
	ctx             sdk.Context
	destDomain      uint32
	destRecipient   string
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

	forwardAddr, err := deriveTestForwardAddress(destDomain, destRecipient)
	require.NoError(t, err)

	signer := sdk.AccAddress([]byte("signer______________"))
	bankKeeper := NewMockBankKeeper()
	warpKeeper := NewMockWarpKeeper()
	hyperlaneKeeper := NewMockHyperlaneKeeper()

	// Setup TIA collateral token with route
	tiaToken := createTestHypToken(1, appconsts.BondDenom, warptypes.HYP_TOKEN_TYPE_COLLATERAL)
	warpKeeper.Tokens = append(warpKeeper.Tokens, tiaToken)
	warpKeeper.EnrolledRouters[1] = map[uint32]warptypes.RemoteRouter{
		destDomain: {Gas: math.NewInt(200000)},
	}

	k := keeper.NewKeeper(bankKeeper, warpKeeper, hyperlaneKeeper)

	return &testIGPSetup{
		ctx:             ctx,
		destDomain:      destDomain,
		destRecipient:   destRecipient,
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
				tc.maxIgpFee,
			)

			resp, err := s.msgServer.Forward(s.ctx, msg)

			require.Error(t, err)
			require.Nil(t, resp)
			require.ErrorIs(t, err, types.ErrAllTokensFailed)
			require.ErrorContains(t, err, tc.expectedErrPart)

			// Verify no balance changes (validation failed before transfers)
			require.Equal(t, math.NewInt(1000), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount)
			require.Equal(t, tc.expectedSignerBal, s.bankKeeper.GetBalance(s.ctx, s.signer, tc.maxIgpFee.Denom).Amount)
		})
	}
}

// TestForwardSingleToken_IGPFeeSentToFeeCollectorOnWarpFailure tests that IGP fee goes to fee collector when warp fails
func TestForwardSingleToken_IGPFeeSentToFeeCollectorOnWarpFailure(t *testing.T) {
	s := newTestIGPSetup(t)

	feeCollectorAddr := authtypes.NewModuleAddress(authtypes.FeeCollectorName)

	// Setup balances
	s.bankKeeper.Balances[s.forwardAddr.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	s.bankKeeper.Balances[s.signer.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(200)))
	s.bankKeeper.Balances[feeCollectorAddr.String()] = sdk.NewCoins() // Start with zero
	s.hyperlaneKeeper.QuotedFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)))

	// Warp transfer will FAIL
	s.warpKeeper.TransferErr = errors.New("warp transfer failed: insufficient liquidity")

	msg := types.NewMsgForward(
		s.signer.String(),
		s.forwardAddr.String(),
		s.destDomain,
		s.destRecipient,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)

	resp, err := s.msgServer.Forward(s.ctx, msg)

	require.Error(t, err)
	require.Nil(t, resp)
	require.ErrorIs(t, err, types.ErrAllTokensFailed)
	require.ErrorContains(t, err, "warp transfer failed")

	// Verify: tokens remain at forwardAddr (warp atomic semantics)
	require.Equal(t, math.NewInt(1000), s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount)
	// Verify: IGP fee was deducted from signer (100 consumed)
	require.Equal(t, math.NewInt(100), s.bankKeeper.GetBalance(s.ctx, s.signer, appconsts.BondDenom).Amount)
	// Verify: IGP fee was sent to fee collector (becomes protocol revenue)
	require.Equal(t, math.NewInt(100), s.bankKeeper.GetBalance(s.ctx, feeCollectorAddr, appconsts.BondDenom).Amount,
		"IGP fee should be sent to fee collector on warp failure")
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

	// Simulate warp consuming only 80 utia (less than quoted 100)
	actualIgpConsumed := math.NewInt(80)
	s.warpKeeper.OnTransfer = func(sender string, maxFee sdk.Coin) {
		senderAddr, _ := sdk.AccAddressFromBech32(sender)
		currentBal := s.bankKeeper.Balances[senderAddr.String()]
		consumed := sdk.NewCoins(sdk.NewCoin(maxFee.Denom, actualIgpConsumed))
		s.bankKeeper.Balances[senderAddr.String()] = currentBal.Sub(consumed...)
	}

	msg := types.NewMsgForward(
		s.signer.String(),
		s.forwardAddr.String(),
		s.destDomain,
		s.destRecipient,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)

	resp, err := s.msgServer.Forward(s.ctx, msg)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Results, 1)
	require.True(t, resp.Results[0].Success)

	// Verify: signer paid 100, got 20 refund, net cost = 80
	// Final signer balance = 200 - 100 + 20 = 120
	require.Equal(t, math.NewInt(120), s.bankKeeper.GetBalance(s.ctx, s.signer, appconsts.BondDenom).Amount,
		"signer should have received refund of excess IGP fee")
}

func TestForward_AllTokensFailedErrorIncludesPerTokenFailures(t *testing.T) {
	s := newTestIGPSetup(t)

	// Two failing tokens:
	// 1) ufoo fails token lookup
	// 2) utia fails due insufficient max_igp_fee
	s.bankKeeper.Balances[s.forwardAddr.String()] = sdk.NewCoins(
		sdk.NewCoin("ufoo", math.NewInt(25)),
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)),
	)
	s.bankKeeper.Balances[s.signer.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(200)))
	s.hyperlaneKeeper.QuotedFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(150)))

	msg := types.NewMsgForward(
		s.signer.String(),
		s.forwardAddr.String(),
		s.destDomain,
		s.destRecipient,
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)),
	)

	resp, err := s.msgServer.Forward(s.ctx, msg)
	require.Error(t, err)
	require.Nil(t, resp)
	require.ErrorIs(t, err, types.ErrAllTokensFailed)

	errText := err.Error()
	require.Contains(t, errText, "all 2 tokens failed to forward")
	require.Contains(t, errText, "ufoo:25 (token lookup failed: unsupported token denom)")
	require.Contains(t, errText, "utia:1000 (IGP fee provided is less than required")
}
