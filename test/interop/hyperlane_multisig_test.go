package interop

import (
	"context"

	"cosmossdk.io/math"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cosmostx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	multisigtypes "github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// TestMultisigSetRoutingIsmDomain is a regression test for
// https://github.com/celestiaorg/celestia-app/issues/6541.
// MsgSetRoutingIsmDomain previously had a proto descriptor registration issue
// that caused multisig transactions (which use SIGN_MODE_LEGACY_AMINO_JSON)
// to fail with "unknown protobuf field" errors.
func (s *HyperlaneTestSuite) TestMultisigSetRoutingIsmDomain() {
	chain := s.celestia

	// Generate two private keys and create a 2-of-2 multisig public key.
	priv1 := secp256k1.GenPrivKey()
	priv2 := secp256k1.GenPrivKey()
	pubKeys := []cryptotypes.PubKey{priv1.PubKey(), priv2.PubKey()}
	msigPubKey := multisig.NewLegacyAminoPubKey(2, pubKeys)
	msigAddr := sdk.AccAddress(msigPubKey.Address())

	// Fund the multisig account. The test infra uses "stake" as the default
	// bond denom.
	fundMsg := &banktypes.MsgSend{
		FromAddress: chain.SenderAccount.GetAddress().String(),
		ToAddress:   msigAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1_000_000))),
	}
	_, err := chain.SendMsgs(fundMsg)
	s.Require().NoError(err)

	// Create a noop ISM to use as the route target.
	noopIsmID := s.SetupNoopISM(chain)

	// Create a routing ISM (owned by the default sender).
	msgCreateRoutingIsm := &ismtypes.MsgCreateRoutingIsm{
		Creator: chain.SenderAccount.GetAddress().String(),
	}
	res, err := chain.SendMsgs(msgCreateRoutingIsm)
	s.Require().NoError(err)

	var createResp ismtypes.MsgCreateRoutingIsmResponse
	err = unmarshalMsgResponses(chain.Codec, res.GetData(), &createResp)
	s.Require().NoError(err)
	routingIsmID := createResp.Id

	// Transfer routing ISM ownership to the multisig address.
	msgTransferOwnership := &ismtypes.MsgUpdateRoutingIsmOwner{
		IsmId:    routingIsmID,
		Owner:    chain.SenderAccount.GetAddress().String(),
		NewOwner: msigAddr.String(),
	}
	_, err = chain.SendMsgs(msgTransferOwnership)
	s.Require().NoError(err)

	// Build the MsgSetRoutingIsmDomain from the multisig.
	msgSetDomain := &ismtypes.MsgSetRoutingIsmDomain{
		IsmId: routingIsmID,
		Owner: msigAddr.String(),
		Route: ismtypes.Route{
			Ism:    noopIsmID,
			Domain: 1,
		},
	}

	// Sign the transaction with the multisig using SIGN_MODE_LEGACY_AMINO_JSON
	// (this is what real multisig accounts use).
	celestiaApp := s.GetCelestiaApp(chain)
	txCfg := celestiaApp.GetTxConfig()
	txBuilder := txCfg.NewTxBuilder()

	err = txBuilder.SetMsgs(msgSetDomain)
	s.Require().NoError(err)
	txBuilder.SetGasLimit(400_000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(0))))

	// Look up the multisig account to get its account number and sequence.
	msigAcc := celestiaApp.AccountKeeper.GetAccount(chain.GetContext(), msigAddr)
	s.Require().NotNil(msigAcc, "multisig account should exist after funding")
	accNum := msigAcc.GetAccountNumber()
	accSeq := msigAcc.GetSequence()

	signMode := signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON

	// Round 1: Set an empty multisig signature to populate signer infos on the
	// tx builder, so that sign bytes are computed correctly.
	emptyMsigData := multisigtypes.NewMultisig(len(pubKeys))
	emptySig := signing.SignatureV2{
		PubKey:   msigPubKey,
		Data:     emptyMsigData,
		Sequence: accSeq,
	}
	err = txBuilder.SetSignatures(emptySig)
	s.Require().NoError(err)

	// Round 2: Each key signs the transaction.
	signerData := authsigning.SignerData{
		Address:       msigAddr.String(),
		ChainID:       chain.ChainID,
		AccountNumber: accNum,
		Sequence:      accSeq,
		PubKey:        msigPubKey,
	}

	sig1, err := cosmostx.SignWithPrivKey(
		context.Background(), signMode, signerData, txBuilder, priv1, txCfg, accSeq,
	)
	s.Require().NoError(err)

	sig2, err := cosmostx.SignWithPrivKey(
		context.Background(), signMode, signerData, txBuilder, priv2, txCfg, accSeq,
	)
	s.Require().NoError(err)

	// Round 3: Aggregate individual signatures into the multisig.
	msigData := multisigtypes.NewMultisig(len(pubKeys))
	err = multisigtypes.AddSignatureV2(msigData, sig1, pubKeys)
	s.Require().NoError(err)
	err = multisigtypes.AddSignatureV2(msigData, sig2, pubKeys)
	s.Require().NoError(err)

	finalSig := signing.SignatureV2{
		PubKey:   msigPubKey,
		Data:     msigData,
		Sequence: accSeq,
	}
	err = txBuilder.SetSignatures(finalSig)
	s.Require().NoError(err)

	// Encode and submit the transaction via FinalizeBlock.
	txBytes, err := txCfg.TxEncoder()(txBuilder.GetTx())
	s.Require().NoError(err)

	resp, err := chain.App.GetBaseApp().FinalizeBlock(&abci.RequestFinalizeBlock{
		Height:             chain.App.GetBaseApp().LastBlockHeight() + 1,
		Time:               chain.CurrentHeader.GetTime(),
		NextValidatorsHash: chain.NextVals.Hash(),
		Txs:                [][]byte{txBytes},
	})
	s.Require().NoError(err)
	s.Require().Len(resp.TxResults, 1)

	txResult := resp.TxResults[0]
	s.Require().Equal(uint32(0), txResult.Code,
		"MsgSetRoutingIsmDomain from multisig should succeed but got code %d: %s",
		txResult.Code, txResult.Log)
}
