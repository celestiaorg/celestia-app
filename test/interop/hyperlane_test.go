package interop

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	hooktypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/app/params"
	minttypes "github.com/celestiaorg/celestia-app/v7/x/mint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/suite"
)

type HyperlaneTestSuite struct {
	suite.Suite

	celestia *ibctesting.TestChain
	chainA   *ibctesting.TestChain
}

func TestHyperlaneTestSuite(t *testing.T) {
	suite.Run(t, new(HyperlaneTestSuite))
}

// SetupTest initializes the test environment for hyperlane tests.
// It sets up celestia and chainA chains, and mints utia tokens on celestia
// since the test infrastructure uses "stake" by default but hyperlane tests require utia.
func (s *HyperlaneTestSuite) SetupTest() {
	_, celestia, simapp, _ := SetupTest(s.T())

	s.celestia = celestia
	s.chainA = simapp

	// NOTE: the test infra funds accounts with token denom "stake" by default, so we mint some utia here
	app := s.GetCelestiaApp(celestia)
	err := app.BankKeeper.MintCoins(celestia.GetContext(), minttypes.ModuleName, sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(1_000_000))))
	s.Require().NoError(err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(celestia.GetContext(), minttypes.ModuleName, celestia.SenderAccount.GetAddress(), sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(1_000_000))))
	s.Require().NoError(err)
}

func (s *HyperlaneTestSuite) GetCelestiaApp(chain *ibctesting.TestChain) *app.App {
	app, ok := chain.App.(*app.App)
	s.Require().True(ok)
	return app
}

func (s *HyperlaneTestSuite) GetSimapp(chain *ibctesting.TestChain) *SimApp {
	app, ok := chain.App.(*SimApp)
	s.Require().True(ok)
	return app
}

// TestHyperlaneOutboundTransfer tests the full hyperlane transfer flow from celestia to chainA and back.
// It sends utia tokens from celestia to chainA, then sends them back to celestia,
// verifying the round-trip maintains balance integrity.
func (s *HyperlaneTestSuite) TestHyperlaneOutboundTransfer() {
	const (
		CelestiaDomainID = 69420
		ChainADomainID   = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	originalBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), s.celestia.SenderAccount.GetAddress(), params.BondDenom)

	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)

	ismIDChainA := s.SetupNoopISM(s.chainA)
	mailboxIDChainA := s.SetupMailBox(s.chainA, ismIDChainA, ChainADomainID)

	// create collateral token (celestia - utia)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// create synthetic token (chainA - hyperlane bridged asset)
	synTokenID := s.CreateSyntheticToken(s.chainA, ismIDChainA, mailboxIDCelestia)

	// enroll remote routers (pairs the utia collateral token with the synthetic token on the chainA counterparty)
	s.EnrollRemoteRouter(s.celestia, collatTokenID, ChainADomainID, synTokenID.String())
	s.EnrollRemoteRouter(s.chainA, synTokenID, CelestiaDomainID, collatTokenID.String())

	// NOTE: Hyperlane HexAddress is expected to be 32 bytes,
	// as cosmos addresses are 20 bytes, we must left-pad the address
	addrBz := make([]byte, 32)
	copy(addrBz[12:], s.chainA.SenderAccount.GetAddress().Bytes())

	msgRemoteTransfer := warptypes.MsgRemoteTransfer{
		Sender:            s.celestia.SenderAccount.GetAddress().String(),
		TokenId:           collatTokenID,
		DestinationDomain: ChainADomainID,
		Recipient:         util.HexAddress(addrBz),
		Amount:            math.NewInt(1000),
	}

	res, err := s.celestia.SendMsgs(&msgRemoteTransfer)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var hypMsg string
	for _, evt := range res.Events {
		// parse the hyperlane message from the dispatch events
		if evt.Type == proto.MessageName(&coretypes.EventDispatch{}) {
			protoMsg, err := sdk.ParseTypedEvent(evt)
			s.Require().NoError(err)

			eventDispatch, ok := protoMsg.(*coretypes.EventDispatch)
			s.Require().True(ok)

			hypMsg = eventDispatch.Message
		}
	}

	// process the msg on the chainA counterparty
	msgProcessMessage := coretypes.MsgProcessMessage{
		MailboxId: mailboxIDChainA,
		Relayer:   s.chainA.SenderAccount.GetAddress().String(),
		Message:   hypMsg,
	}

	res, err = s.chainA.SendMsgs(&msgProcessMessage)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	chainAApp := s.GetSimapp(s.chainA)
	hypDenom, err := chainAApp.WarpKeeper.HypTokens.Get(s.chainA.GetContext(), synTokenID.GetInternalId())
	s.Require().NoError(err)

	balance := chainAApp.BankKeeper.GetBalance(s.chainA.GetContext(), s.chainA.SenderAccount.GetAddress(), hypDenom.OriginDenom)
	s.Require().Equal(math.NewInt(1000).Int64(), balance.Amount.Int64())

	// check that the account on celestia has "amount" less tokens than before
	intermediateBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), s.celestia.SenderAccount.GetAddress(), params.BondDenom)
	want := originalBalance.Amount.Sub(math.NewInt(1000))
	s.Require().Equal(want, intermediateBalance.Amount)

	// Send the tokens back from chainA to celestia
	addrBzCelestia := make([]byte, 32)
	copy(addrBzCelestia[12:], s.celestia.SenderAccount.GetAddress().Bytes())

	msgRemoteTransferBack := warptypes.MsgRemoteTransfer{
		Sender:            s.chainA.SenderAccount.GetAddress().String(),
		TokenId:           synTokenID,
		DestinationDomain: CelestiaDomainID,
		Recipient:         util.HexAddress(addrBzCelestia),
		Amount:            math.NewInt(1000),
	}

	res, err = s.chainA.SendMsgs(&msgRemoteTransferBack)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var hypMsgBack string
	for _, evt := range res.Events {
		// parse the hyperlane message from the dispatch events
		if evt.Type == proto.MessageName(&coretypes.EventDispatch{}) {
			protoMsg, err := sdk.ParseTypedEvent(evt)
			s.Require().NoError(err)

			eventDispatch, ok := protoMsg.(*coretypes.EventDispatch)
			s.Require().True(ok)

			hypMsgBack = eventDispatch.Message
		}
	}

	// process the msg on the celestia counterparty
	msgProcessMessageBack := coretypes.MsgProcessMessage{
		MailboxId: mailboxIDCelestia,
		Relayer:   s.celestia.SenderAccount.GetAddress().String(),
		Message:   hypMsgBack,
	}

	res, err = s.celestia.SendMsgs(&msgProcessMessageBack)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// check that the token was sent back i.e. the new balance is equal to the original balance
	newBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), s.celestia.SenderAccount.GetAddress(), params.BondDenom)
	s.Require().Equal(originalBalance, newBalance)

	// check that the chainA balance is 0 after sending back the token
	finalChainABalance := chainAApp.BankKeeper.GetBalance(s.chainA.GetContext(), s.chainA.SenderAccount.GetAddress(), hypDenom.OriginDenom)
	s.Require().Equal(math.NewInt(0).Int64(), finalChainABalance.Amount.Int64())
}

// TestHyperlaneInboundTransfer tests the full hyperlane transfer flow from chainA to celestia and back.
// It sends native tokens from chainA to celestia, then sends them back to chainA,
// verifying the round-trip maintains balance integrity.
func (s *HyperlaneTestSuite) TestHyperlaneInboundTransfer() {
	const (
		CelestiaDomainID = 69420
		ChainADomainID   = 1337
	)

	chainAApp := s.GetSimapp(s.chainA)
	originalBalance := chainAApp.BankKeeper.GetBalance(s.chainA.GetContext(), s.chainA.SenderAccount.GetAddress(), sdk.DefaultBondDenom)

	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)

	ismIDChainA := s.SetupNoopISM(s.chainA)
	mailboxIDChainA := s.SetupMailBox(s.chainA, ismIDChainA, ChainADomainID)

	// create collateral token (chainA - stake)
	collatTokenID := s.CreateCollateralToken(s.chainA, ismIDChainA, mailboxIDChainA, sdk.DefaultBondDenom)

	// create synthetic token (celestia - hyperlane bridged asset)
	synTokenID := s.CreateSyntheticToken(s.celestia, ismIDCelestia, mailboxIDChainA)

	// enroll remote routers (pairs the stake collateral token with the synthetic token on the celestia counterparty)
	s.EnrollRemoteRouter(s.chainA, collatTokenID, CelestiaDomainID, synTokenID.String())
	s.EnrollRemoteRouter(s.celestia, synTokenID, ChainADomainID, collatTokenID.String())

	// NOTE: Hyperlane HexAddress is expected to be 32 bytes,
	// as cosmos addresses are 20 bytes, we must left-pad the address
	addrBz := make([]byte, 32)
	copy(addrBz[12:], s.celestia.SenderAccount.GetAddress().Bytes())

	msgRemoteTransfer := warptypes.MsgRemoteTransfer{
		Sender:            s.chainA.SenderAccount.GetAddress().String(),
		TokenId:           collatTokenID,
		DestinationDomain: CelestiaDomainID,
		Recipient:         util.HexAddress(addrBz),
		Amount:            math.NewInt(1000),
	}

	res, err := s.chainA.SendMsgs(&msgRemoteTransfer)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var hypMsg string
	for _, evt := range res.Events {
		// parse the hyperlane message from the dispatch events
		if evt.Type == proto.MessageName(&coretypes.EventDispatch{}) {
			protoMsg, err := sdk.ParseTypedEvent(evt)
			s.Require().NoError(err)

			eventDispatch, ok := protoMsg.(*coretypes.EventDispatch)
			s.Require().True(ok)

			hypMsg = eventDispatch.Message
		}
	}

	// process the msg on the celestia counterparty
	msgProcessMessage := coretypes.MsgProcessMessage{
		MailboxId: mailboxIDCelestia,
		Relayer:   s.celestia.SenderAccount.GetAddress().String(),
		Message:   hypMsg,
	}

	res, err = s.celestia.SendMsgs(&msgProcessMessage)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	hypDenom, err := celestiaApp.WarpKeeper.HypTokens.Get(s.celestia.GetContext(), synTokenID.GetInternalId())
	s.Require().NoError(err)

	balance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), s.celestia.SenderAccount.GetAddress(), hypDenom.OriginDenom)
	s.Require().Equal(math.NewInt(1000).Int64(), balance.Amount.Int64())

	// check that the account on chainA has "amount" less tokens than before
	intermediateBalance := chainAApp.BankKeeper.GetBalance(s.chainA.GetContext(), s.chainA.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	want := originalBalance.Amount.Sub(math.NewInt(1000))
	s.Require().Equal(want, intermediateBalance.Amount)

	// Send the tokens back from celestia to chainA
	addrBzChainA := make([]byte, 32)
	copy(addrBzChainA[12:], s.chainA.SenderAccount.GetAddress().Bytes())

	msgRemoteTransferBack := warptypes.MsgRemoteTransfer{
		Sender:            s.celestia.SenderAccount.GetAddress().String(),
		TokenId:           synTokenID,
		DestinationDomain: ChainADomainID,
		Recipient:         util.HexAddress(addrBzChainA),
		Amount:            math.NewInt(1000),
	}

	res, err = s.celestia.SendMsgs(&msgRemoteTransferBack)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var hypMsgBack string
	for _, evt := range res.Events {
		// parse the hyperlane message from the dispatch events
		if evt.Type == proto.MessageName(&coretypes.EventDispatch{}) {
			protoMsg, err := sdk.ParseTypedEvent(evt)
			s.Require().NoError(err)

			eventDispatch, ok := protoMsg.(*coretypes.EventDispatch)
			s.Require().True(ok)

			hypMsgBack = eventDispatch.Message
		}
	}

	// process the msg on the chainA counterparty
	msgProcessMessageBack := coretypes.MsgProcessMessage{
		MailboxId: mailboxIDChainA,
		Relayer:   s.chainA.SenderAccount.GetAddress().String(),
		Message:   hypMsgBack,
	}

	res, err = s.chainA.SendMsgs(&msgProcessMessageBack)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// check that the token was sent back i.e. the new balance is equal to the original balance
	newBalance := chainAApp.BankKeeper.GetBalance(s.chainA.GetContext(), s.chainA.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	s.Require().Equal(originalBalance, newBalance)

	// check that the celestia balance is 0 after sending back the token
	finalCelestiaBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), s.celestia.SenderAccount.GetAddress(), hypDenom.OriginDenom)
	s.Require().Equal(math.NewInt(0).Int64(), finalCelestiaBalance.Amount.Int64())
}

func (s *HyperlaneTestSuite) SetupNoopISM(chain *ibctesting.TestChain) util.HexAddress {
	msgCreateNoopISM := &ismtypes.MsgCreateNoopIsm{
		Creator: chain.SenderAccount.GetAddress().String(),
	}

	res, err := chain.SendMsgs(msgCreateNoopISM)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var resp ismtypes.MsgCreateNoopIsmResponse
	err = unmarshalMsgResponses(chain.Codec, res.GetData(), &resp)
	s.Require().NoError(err)

	return resp.Id
}

func (s *HyperlaneTestSuite) SetupMailBox(chain *ibctesting.TestChain, ismID util.HexAddress, domain uint32) util.HexAddress {
	msgCreateNoopHooks := &hooktypes.MsgCreateNoopHook{
		Owner: chain.SenderAccount.GetAddress().String(),
	}

	res, err := chain.SendMsgs(msgCreateNoopHooks)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var respHooks hooktypes.MsgCreateNoopHookResponse
	err = unmarshalMsgResponses(chain.Codec, res.GetData(), &respHooks)
	s.Require().NoError(err)

	msgCreateMailbox := &coretypes.MsgCreateMailbox{
		Owner:        chain.SenderAccount.GetAddress().String(),
		LocalDomain:  domain,
		DefaultIsm:   ismID,
		DefaultHook:  &respHooks.Id,
		RequiredHook: &respHooks.Id,
	}

	res, err = chain.SendMsgs(msgCreateMailbox)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var respMailbox coretypes.MsgCreateMailboxResponse
	err = unmarshalMsgResponses(chain.Codec, res.GetData(), &respMailbox)
	s.Require().NoError(err)

	return respMailbox.Id
}

func (s *HyperlaneTestSuite) CreateCollateralToken(chain *ibctesting.TestChain, ismID, mailboxID util.HexAddress, denom string) util.HexAddress {
	msgCreateCollateralToken := warptypes.MsgCreateCollateralToken{
		Owner:         chain.SenderAccount.GetAddress().String(),
		OriginMailbox: mailboxID,
		OriginDenom:   denom,
	}

	res, err := chain.SendMsgs(&msgCreateCollateralToken)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var resp warptypes.MsgCreateCollateralTokenResponse
	err = unmarshalMsgResponses(chain.Codec, res.GetData(), &resp)
	s.Require().NoError(err)

	// set ism id on new collateral token (for some reason this can't be done on creation)
	msgSetToken := warptypes.MsgSetToken{
		Owner:    chain.SenderAccount.GetAddress().String(),
		TokenId:  resp.Id,
		IsmId:    &ismID,
		NewOwner: chain.SenderAccount.GetAddress().String(),
	}

	res, err = chain.SendMsgs(&msgSetToken)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	return resp.Id
}

func (s *HyperlaneTestSuite) CreateSyntheticToken(chain *ibctesting.TestChain, ismID, mailboxID util.HexAddress) util.HexAddress {
	msgCreateSyntheticToken := warptypes.MsgCreateSyntheticToken{
		Owner:         chain.SenderAccount.GetAddress().String(),
		OriginMailbox: mailboxID,
	}

	res, err := chain.SendMsgs(&msgCreateSyntheticToken)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var resp warptypes.MsgCreateSyntheticTokenResponse
	err = unmarshalMsgResponses(chain.Codec, res.GetData(), &resp)
	s.Require().NoError(err)

	// set ism id on new synthetic token (for some reason this can't be done on creation)
	msgSetToken := warptypes.MsgSetToken{
		Owner:    chain.SenderAccount.GetAddress().String(),
		TokenId:  resp.Id,
		IsmId:    &ismID,
		NewOwner: chain.SenderAccount.GetAddress().String(),
	}

	res, err = chain.SendMsgs(&msgSetToken)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	return resp.Id
}

func (s *HyperlaneTestSuite) EnrollRemoteRouter(chain *ibctesting.TestChain, tokenID util.HexAddress, domain uint32, recvContract string) {
	remoteRouter := &warptypes.RemoteRouter{
		ReceiverDomain:   domain,
		ReceiverContract: recvContract,
		Gas:              math.ZeroInt(),
	}

	msgEnrollRemoteRouter := warptypes.MsgEnrollRemoteRouter{
		Owner:        chain.SenderAccount.GetAddress().String(),
		TokenId:      tokenID,
		RemoteRouter: remoteRouter,
	}

	res, err := chain.SendMsgs(&msgEnrollRemoteRouter)
	s.Require().NoError(err)
	s.Require().NotNil(res)
}

func unmarshalMsgResponses(cdc codec.Codec, data []byte, msgs ...proto.Message) error {
	var txMsgData sdk.TxMsgData
	if err := cdc.Unmarshal(data, &txMsgData); err != nil {
		return err
	}

	if len(msgs) != len(txMsgData.MsgResponses) {
		return fmt.Errorf("expected %d message responses but got %d", len(msgs), len(txMsgData.MsgResponses))
	}

	for i, msg := range msgs {
		if err := cdc.Unmarshal(txMsgData.MsgResponses[i].Value, msg); err != nil {
			return err
		}
	}

	return nil
}
