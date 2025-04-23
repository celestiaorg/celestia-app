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
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/params"
	minttypes "github.com/celestiaorg/celestia-app/v4/x/mint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/suite"
)

type HyperlaneTestSuite struct {
	suite.Suite

	coordinator *ibctesting.Coordinator
	celestia    *ibctesting.TestChain
	simapp      *ibctesting.TestChain
}

func TestHyperlaneTestSuite(t *testing.T) {
	suite.Run(t, new(HyperlaneTestSuite))
}

func (s *HyperlaneTestSuite) SetupTest() {
	coordinator, simapp, celestia, _ := SetupTest(s.T())

	s.coordinator = coordinator
	s.celestia = celestia
	s.simapp = simapp

	app := celestia.App.(*app.App)
	err := app.BankKeeper.MintCoins(celestia.GetContext(), minttypes.ModuleName, sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(1_000_000))))
	s.Require().NoError(err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(celestia.GetContext(), minttypes.ModuleName, celestia.SenderAccount.GetAddress(), sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(1_000_000))))
	s.Require().NoError(err)
}

func (s *HyperlaneTestSuite) TestHyperlaneTransfer() {
	ismIDCelestia, ismIDSimapp := s.SetupNoopISM(s.celestia), s.SetupNoopISM(s.simapp)

	// setup mailboxes
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia)
	mailboxIDSimapp := s.SetupMailBox(s.simapp, ismIDSimapp)

	// create collateral token (celestia)
	collatTokenID := s.CreateCollateralToken(s.celestia, mailboxIDCelestia)

	// set ism id on new collateral token
	msgSetToken := warptypes.MsgSetToken{
		Owner:    s.celestia.SenderAccount.GetAddress().String(),
		TokenId:  collatTokenID,
		IsmId:    &ismIDCelestia,
		NewOwner: s.celestia.SenderAccount.GetAddress().String(),
	}

	res, err := s.celestia.SendMsgs(&msgSetToken)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// create synethetic token (simapp)
	synTokenID := s.CreateSyntheticToken(s.simapp, mailboxIDCelestia)

	// set ism id on new synthetic token
	msgSetToken = warptypes.MsgSetToken{
		Owner:    s.simapp.SenderAccount.GetAddress().String(),
		TokenId:  synTokenID,
		IsmId:    &ismIDSimapp,
		NewOwner: s.simapp.SenderAccount.GetAddress().String(),
	}

	res, err = s.simapp.SendMsgs(&msgSetToken)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// enroll remote router
	msgEnrollRemoteRouter := warptypes.MsgEnrollRemoteRouter{
		Owner:   s.celestia.SenderAccount.GetAddress().String(),
		TokenId: collatTokenID,
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   69420,
			ReceiverContract: mailboxIDSimapp.String(),
			Gas:              math.ZeroInt(),
		},
	}

	res, err = s.celestia.SendMsgs(&msgEnrollRemoteRouter)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// simapp enroll router ??

	// send transfer
	msgRemoteTransfer := warptypes.MsgRemoteTransfer{
		Sender:            s.celestia.SenderAccount.GetAddress().String(),
		TokenId:           collatTokenID,
		DestinationDomain: 69420,
		Recipient:         ismIDSimapp, // TODO: figure out this field
		Amount:            math.NewInt(1000),
	}

	res, err = s.celestia.SendMsgs(&msgRemoteTransfer)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var hyperlaneMsg string
	for _, evt := range res.Events {
		if evt.Type == proto.MessageName(&coretypes.EventDispatch{}) {
			protoMsg, err := sdk.ParseTypedEvent(evt)
			s.Require().NoError(err)

			eventDispatch, ok := protoMsg.(*coretypes.EventDispatch)
			s.Require().True(ok)

			s.T().Logf("EventDispatch: %s", eventDispatch)
			hyperlaneMsg = eventDispatch.Message
		}
	}

	// process msg on simapp
	msgProcessMsg := coretypes.MsgProcessMessage{
		MailboxId: mailboxIDSimapp,
		Relayer:   s.simapp.SenderAccount.GetAddress().String(),
		Message:   hyperlaneMsg,
	}

	res, err = s.simapp.SendMsgs(&msgProcessMsg)
	s.Require().NoError(err)
	s.Require().NotNil(res)
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

func (s *HyperlaneTestSuite) SetupMailBox(chain *ibctesting.TestChain, ismID util.HexAddress) util.HexAddress {
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
		LocalDomain:  69420, // TODO: hardcode domains for now (doesn't matter)
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

func (s *HyperlaneTestSuite) CreateCollateralToken(chain *ibctesting.TestChain, mailboxID util.HexAddress) util.HexAddress {
	msgCreateCollateralToken := warptypes.MsgCreateCollateralToken{
		Owner:         chain.SenderAccount.GetAddress().String(),
		OriginMailbox: mailboxID,
		OriginDenom:   params.BondDenom,
	}

	res, err := chain.SendMsgs(&msgCreateCollateralToken)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	var resp warptypes.MsgCreateCollateralTokenResponse
	err = unmarshalMsgResponses(chain.Codec, res.GetData(), &resp)
	s.Require().NoError(err)

	return resp.Id
}

func (s *HyperlaneTestSuite) CreateSyntheticToken(chain *ibctesting.TestChain, mailboxID util.HexAddress) util.HexAddress {
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

	return resp.Id
}

func unmarshalMsgResponses(cdc codec.Codec, data []byte, msgs ...codec.ProtoMarshaler) error {
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
