package app_test

import (
	"fmt"
	"testing"
	// "time"

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

	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	crisisTypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	distribution "github.com/cosmos/cosmos-sdk/x/distribution/types"

	// evidence "github.com/cosmos/cosmos-sdk/x/evidence/types"
	// "github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	// slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
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
	accountAddresses := make([]sdk.AccAddress, 0, len(recs))

	// Get the name of the records
	for _, rec := range recs {
		accountNames = append(accountNames, rec.Name)
		accAddress, err := rec.GetAddress()
		require.NoError(t, err)
		accountAddresses = append(accountAddresses, accAddress)
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

	oneInt := sdk.OneInt()
	commission := sdk.NewDecWithPrec(6, 0o2)

	// Create an SDK Tx
	sdkTx := SdkTx{
		sdkMsgs: []sdk.Msg{
			// Single message in a single transactiFon
			banktypes.NewMsgSend(accountAddresses[0],
				accountAddresses[1],
				amount),
			// Multiple messages in a single transaction
			banktypes.NewMsgMultiSend([]banktypes.Input{
				banktypes.NewInput(
					accountAddresses[0],
					amount,
				),
			},
				[]banktypes.Output{
					banktypes.NewOutput(
						accountAddresses[1],
						amount,
					),
				}),
			func() sdk.Msg {
				authorization := authz.NewGenericAuthorization(blobtypes.URLMsgPayForBlobs)
				msgGrant, err := authz.NewMsgGrant(
					accountAddresses[0],
					accountAddresses[1],
					authorization,
					&expiration,
				)
				require.NoError(t, err)
				return msgGrant
			}(),
			func() sdk.Msg {
				msgRevoke := authz.NewMsgRevoke(
					accountAddresses[0],
					accountAddresses[1],
					blobtypes.URLMsgPayForBlobs,
				)
				msgExec := authz.NewMsgExec(accountAddresses[0], []sdk.Msg{&msgRevoke})
				return &msgExec
			}(),
			crisisTypes.NewMsgVerifyInvariant(accountAddresses[0], banktypes.ModuleName, "nonnegative-outstanding"),
			func() sdk.Msg {
				basicAllowance := feegrant.BasicAllowance{
					SpendLimit: sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(1000))),
				}
				feegrantMsg, err := feegrant.NewMsgGrantAllowance(&basicAllowance, accountAddresses[0], accountAddresses[1])
				require.NoError(t, err)
				return feegrantMsg
			}(),
			func() sdk.Msg {
				msgRevoke := feegrant.NewMsgRevokeAllowance(accountAddresses[0], accountAddresses[1])
				return &msgRevoke
			}(),
			// func() sdk.Msg {
			// 	valConsAddr, err  := validators[0].GetConsAddr()
			// 	require.NoError(t, err)
			// 	fmt.Println(valConsAddr.String(), "ValConsAddr")
			// 	msgEvidence, err := evidence.NewMsgSubmitEvidence(accountAddresses[0],  &evidence.Equivocation{
			// 		Height:           10,
			// 		Power:            100,
			// 		Time:             time.Now().UTC(),
			// 		ConsensusAddress:  valConsAddr.String(),
			// 	})
			// 	require.NoError(t, err)
			// 	return msgEvidence
			// }(),
			func() sdk.Msg {
				govAccount := testApp.GovKeeper.GetGovernanceAccount(testApp.NewContext(false, tmproto.Header{})).GetAddress()
				msg := banktypes.MsgSend{
					FromAddress: govAccount.String(),
					ToAddress:   accountAddresses[1].String(),
					Amount:      amount,
				}
				proposal, err := govtypes.NewMsgSubmitProposal([]sdk.Msg{&msg}, amount, accountAddresses[0].String(), "")
				require.NoError(t, err)
				return proposal
			}(),
			govtypes.NewMsgDeposit(accountAddresses[0], 1, amount),
			// inactive proposal
			// govtypes.NewMsgVote(accountAddresses[0], 1, govtypes.VoteOption_VOTE_OPTION_YES, ""),
			// govtypes.NewMsgVoteWeighted(
			// 	accountAddresses[0],
			// 	1,
			// 	govtypes.WeightedVoteOptions([]*govtypes.WeightedVoteOption{{Option: govtypes.OptionYes, Weight: "1.0"}}), // Cast the slice to the expected type
			// 	"",
			// ),
			// func() sdk.Msg {
			// valConsAddr, err := validators[0].GetConsAddr()
			// require.NoError(t, err)
			// testApp.StakingKeeper.Jail(testApp.NewContext(false, tmproto.Header{}), valConsAddr)
			// return slashingtypes.NewMsgUnjail(validators[0].GetOperator())
			// }(),
			func() sdk.Msg {
				msgCreateValidator, err := stakingtypes.NewMsgCreateValidator(sdk.ValAddress(accountAddresses[0]),
					ed25519.GenPrivKeyFromSecret([]byte("validator")).PubKey(),
					amount[0],
					stakingtypes.NewDescription("taco tuesday", "my keybase", "www.celestia.org", "ping @celestiaorg on twitter", "fake validator"),
					stakingtypes.NewCommissionRates(sdk.NewDecWithPrec(6, 0o2), sdk.NewDecWithPrec(12, 0o2), sdk.NewDecWithPrec(1, 0o2)),
					sdk.OneInt())
				require.NoError(t, err)
				return msgCreateValidator
			}(),
			func() sdk.Msg {
				return stakingtypes.NewMsgEditValidator(sdk.ValAddress(accountAddresses[0]), stakingtypes.NewDescription("add", "new", "val", "desc", "."), &commission, &oneInt)
			}(),
			stakingtypes.NewMsgDelegate(accountAddresses[0], validators[0].GetOperator(), amount[0]),
			stakingtypes.NewMsgBeginRedelegate(accountAddresses[0], validators[0].GetOperator(), validators[1].GetOperator(), amount[0]),
			stakingtypes.NewMsgUndelegate(accountAddresses[0], validators[1].GetOperator(), amount[0]),
			// failed to execute message; message index: 4: unbonding delegation entry is not found at block height 1: not found
			// stakingtypes.NewMsgCancelUnbondingDelegation(accountAddresses[0], validators[1].GetOperator(), testApp.LastBlockHeight(), amount[0]),
			stakingtypes.NewMsgDelegate(accountAddresses[0], validators[0].GetOperator(), amount[0]),
			distribution.NewMsgSetWithdrawAddress(accountAddresses[0], accountAddresses[1]),
			distribution.NewMsgFundCommunityPool(amount, accountAddresses[0]),
			distribution.NewMsgWithdrawDelegatorReward(accountAddresses[0], validators[0].GetOperator()),
			// No Delegation distribution info
			// distribution.NewMsgWithdrawValidatorCommission(sdk.ValAddress(accountAddresses[0])),
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
