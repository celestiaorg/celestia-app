package app_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	"github.com/celestiaorg/go-square/blob"
	appns "github.com/celestiaorg/go-square/namespace"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	// authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	// crisisTypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	distribution "github.com/cosmos/cosmos-sdk/x/distribution/types"
	// evidence "github.com/cosmos/cosmos-sdk/x/evidence/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	// slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	// govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	// TODO: Find out if we want to test this
	// upgrade "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	// "time"
)

type SdkTx struct {
	sdkMsgs   []sdk.Msg
	txOptions []user.TxOption
}

type BlobTx struct {
	author    string
	blobs     []*blob.Blob
	txOptions []user.TxOption
}

// var expiration = time.Now().Add(time.Hour)

// TestConsistentAppHash executes a set of txs, generates an app hash,
// and compares it against a previously generated hash from the same set of transactions.
// App hashes across different commits should be consistent.
func TestConsistentAppHash(t *testing.T) {
	// Expected app hash produced by v1.x - TODO: link to the test producing the hash
	// expectedAppHash := []byte{9, 208, 117, 101, 108, 61, 146, 58, 26, 190, 199, 124, 76, 178, 84, 74, 54, 159, 76, 187, 2, 169, 128, 87, 70, 78, 8, 192, 28, 144, 116, 117}

	// Initialize testApp
	testApp := testutil.NewTestApp()

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	// Create deterministic keys
	kr, pubKeys := deterministicKeyRing(enc.Codec)

	recs, err := kr.List()
	require.NoError(t, err)
	accountNames := make([]string, 0, len(recs))

	// Get the name of the records
	for _, rec := range recs {
		accountNames = append(accountNames, rec.Name)
	}

	// Apply genesis state to the app.
	_, _, err = testutil.SetupDeterministicGenesisState(testApp, pubKeys, 1_000_000_000, app.DefaultInitialConsensusParams())
	require.NoError(t, err)

	// Query keyring account infos
	accountInfos := queryAccountInfo(testApp, accountNames, kr)

	// Create accounts for the signer
	accounts := make([]*user.Account, 0, len(accountInfos))
	for i, accountInfo := range accountInfos {
		account := user.NewAccount(accountNames[i], accountInfo.AccountNum, accountInfo.Sequence)
		accounts = append(accounts, account)
	}

	// Create a signer with keyring accounts
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, app.DefaultInitialVersion, accounts...)
	require.NoError(t, err)

	amount := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(1000)))

	validators := testApp.StakingKeeper.GetAllValidators(testApp.NewContext(false, tmproto.Header{}))

	authorization := authz.NewGenericAuthorization(blobtypes.URLMsgPayForBlobs)
	msgGrant, err := authz.NewMsgGrant(
		signer.Account(accountNames[0]).Address(),
		signer.Account(accountNames[1]).Address(),
		authorization,
		&expiration,
	)

	msgRevoke := authz.NewMsgRevoke(
		signer.Account(accountNames[0]).Address(),
		signer.Account(accountNames[1]).Address(),
		blobtypes.URLMsgPayForBlobs,
	)

	msgExec := authz.NewMsgExec(signer.Account(accountNames[0]).Address(), []sdk.Msg{&msgRevoke})

	feegrantMsg, err := feegrant.NewMsgGrantAllowance(&feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(1000))),
	}, signer.Account(accountNames[0]).Address(), signer.Account(accountNames[1]).Address())
	require.NoError(t, err)

	// revoke
	feeGrantRevoke := feegrant.NewMsgRevokeAllowance(signer.Account(accountNames[0]).Address(), signer.Account(accountNames[1]).Address())

	// submit proposal
	// TODO: Fix this
	// proposal, err := govtypes.NewMsgSubmitProposal([]sdk.Msg{banktypes.NewMsgSend(signer.Account(accountNames[0]).Address(),
	// 	signer.Account(accountNames[1]).Address(),
	// 	amount)}, amount, signer.Account(accountNames[3]).Address().String(), "")
	// require.NoError(t, err)

	// Create an SDK Tx
	sdkTx := SdkTx{
		sdkMsgs: []sdk.Msg{
			// Single message in a single transaction
			banktypes.NewMsgSend(signer.Account(accountNames[0]).Address(),
				signer.Account(accountNames[1]).Address(),
				amount),
			// Multiple messages in a single transaction
			banktypes.NewMsgMultiSend([]banktypes.Input{
				banktypes.NewInput(
					signer.Account(accountNames[0]).Address(),
					amount,
				),
			},
				[]banktypes.Output{
					banktypes.NewOutput(
						signer.Account(accountNames[1]).Address(),
						amount,
					),
				}),
			msgGrant,
			&msgExec,
			// TODO: figure out how to test invariants correctly
			// crisisTypes.NewMsgVerifyInvariant(signer.Account(accountNames[0]).Address(), banktypes.ModuleName, ),
			distribution.NewMsgFundCommunityPool(amount, signer.Account(accountNames[0]).Address()),
			distribution.NewMsgSetWithdrawAddress(signer.Account(accountNames[0]).Address(), signer.Account(accountNames[1]).Address()),
			// TODO: figure out how to withdraw delegator reward
			// distribution.NewMsgWithdrawDelegatorReward(signer.Account(accountNames[0]).Address(), validators[0].GetOperator()),
			// TODO: figure out how to withdraw validator commission
			// distribution.NewMsgWithdrawValidatorCommission(validators[2].GetOperator()),
			// TODO: figure out how to submit evidence properly
			// evidence.NewMsgSubmitEvidence(signer.Account(accountNames[0]).Address(), evidence.Equivocation{}),
			feegrantMsg,
			&feeGrantRevoke,
			// proposal,
			// govtypes.NewMsgVote(signer.Account(accountNames[0]).Address(), 0, govtypes.OptionYes, ""),
			// govtypes.NewMsgVoteWeighted(signer.Account(accountNames[0]).Address(), 0, []govtypes.WeightedVoteOption{{Option: govtypes.OptionYes, Weight: "1.0"}}, "")
			// govtypes.NewMsgDeposit(signer.Account(accountNames[0]).Address(), 0, amount),
			// TODO: Fix slashing
			// slashingtypes.NewMsgUnjail(validators[0].GetOperator()),
			// TODO: Fix staking
			// stakingtypes.NewMsgCreateValidator(),
			// stakintypes.NewMsgEditValidator(),
			stakingtypes.NewMsgDelegate(signer.Account(accountNames[0]).Address(), validators[0].GetOperator(), amount[0]),
			stakingtypes.NewMsgBeginRedelegate(signer.Account(accountNames[0]).Address(), validators[0].GetOperator(), validators[1].GetOperator(), amount[0]),
			stakingtypes.NewMsgUndelegate(signer.Account(accountNames[0]).Address(), validators[1].GetOperator(), amount[0]),
			// stakingtypes.NewMsgCancelUnbondingDelegation(signer.Account(accountNames[0]).Address(), validators[1].GetOperator(), amount[0].Amount.Int64()),
		},
		txOptions: blobfactory.DefaultTxOpts(),
	}

	// Create a Blob Tx
	blobTx := BlobTx{
		author:    accountNames[2],
		blobs:     []*blob.Blob{blob.New(fixedNamespace(), []byte{1}, appconsts.DefaultShareVersion)},
		txOptions: blobfactory.DefaultTxOpts(),
	}

	// Create SDK Tx
	rawSdkTx, err := signer.CreateTx(sdkTx.sdkMsgs, sdkTx.txOptions...)
	require.NoError(t, err)

	// Create Blob Tx
	rawBlobTx, _, err := signer.CreatePayForBlobs(blobTx.author, blobTx.blobs, blobTx.txOptions...)
	require.NoError(t, err)

	// BeginBlock
	header := tmproto.Header{
		Version: version.Consensus{App: 1},
		Height:  testApp.LastBlockHeight() + 1,
	}
	testApp.BeginBlock(abci.RequestBeginBlock{Header: header})

	// Deliver SDK Tx
	resp := testApp.DeliverTx(abci.RequestDeliverTx{Tx: rawSdkTx})
	require.EqualValues(t, 0, resp.Code, resp.Log)

	// Deliver Blob Tx
	blob, isBlobTx := blob.UnmarshalBlobTx(rawBlobTx)
	require.True(t, isBlobTx)
	resp = testApp.DeliverTx(abci.RequestDeliverTx{Tx: blob.Tx})
	require.EqualValues(t, 0, resp.Code, resp.Log)

	// EndBlock
	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})

	// Commit the state
	testApp.Commit()

	// Get the app hash
	appHash := testApp.LastCommitID().Hash
	fmt.Println("App Hash: ", appHash)

	// Require that the app hash is equal to the app hash produced on a different commit
	// require.Equal(t, expectedAppHash, appHash)
}

// fixedNamespace returns a hardcoded namespace
func fixedNamespace() appns.Namespace {
	return appns.Namespace{
		Version: 0,
		ID:      []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 37, 67, 154, 200, 228, 130, 74, 147, 162, 11},
	}
}

// deterministicKeyRing returns a deterministic keyring and a list of deterministic public keys
func deterministicKeyRing(cdc codec.Codec) (keyring.Keyring, []types.PubKey) {
	mnemonics := []string{
		"great myself congress genuine scale muscle view uncover pipe miracle sausage broccoli lonely swap table foam brand turtle comic gorilla firm mad grunt hazard",
		"cheap job month trigger flush cactus chest juice dolphin people limit crunch curious secret object beach shield snake hunt group sketch cousin puppy fox",
		"oil suffer bamboo one better attack exist dolphin relief enforce cat asset raccoon lava regret found love certain plunge grocery accuse goat together kiss",
		"giraffe busy subject doll jump drama sea daring again club spend toe mind organ real liar permit refuse change opinion donkey job cricket speed",
		"fee vapor thing fish fan memory negative raven cram win quantum ozone job mirror shoot sting quiz black apart funny sort cancel friend curtain",
		"skin beef review pilot tooth act any alarm there only kick uniform ticket material cereal radar ethics list unlock method coral smooth street frequent",
		"ecology scout core guard load oil school effort near alcohol fancy save cereal owner enforce impact sand husband trophy solve amount fish festival sell",
		"used describe angle twin amateur pyramid bitter pool fluid wing erode rival wife federal curious drink battle put elbow mandate another token reveal tone",
		"reason fork target chimney lift typical fine divorce mixture web robot kiwi traffic stove miss crane welcome camp bless fuel october riot pluck ordinary",
		"undo logic mobile modify master force donor rose crumble forget plate job canal waste turn damp sure point deposit hazard quantum car annual churn",
		"charge subway treat loop donate place loan want grief leg message siren joy road exclude match empty enforce vote meadow enlist vintage wool involve",
	}
	kb := keyring.NewInMemory(cdc)
	pubKeys := make([]types.PubKey, len(mnemonics))
	for idx, mnemonic := range mnemonics {
		rec, err := kb.NewAccount(fmt.Sprintf("account-%d", idx), mnemonic, "", "", hd.Secp256k1)
		if err != nil {
			panic(err)
		}
		pubKey, err := rec.GetPubKey()
		if err != nil {
			panic(err)
		}
		pubKeys[idx] = pubKey
	}
	return kb, pubKeys
}
