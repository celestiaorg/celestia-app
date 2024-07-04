package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	"github.com/celestiaorg/celestia-app/v2/test/util"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"

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
	core "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
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

	accountNames := testfactory.GetAccountNames(kr)
	accountAddresses := testfactory.GetAddresses(kr)

	// Apply genesis state to the app.
	valKeyRing, _, err := testutil.SetupDeterministicGenesisState(testApp, pubKeys, 20_000_000_000, app.DefaultInitialConsensusParams())
	require.NoError(t, err)

	// Query keyring account infos
	accountInfos := queryAccountInfo(testApp, accountNames, kr)

	// Create accounts for the signer
	accounts := CreateAccounts(accountInfos, accountNames)

	// Validator accounts from genesis
	genValidators := testApp.StakingKeeper.GetAllValidators(testApp.NewContext(false, tmproto.Header{}))

	valAccountNames := testfactory.GetAccountNames(valKeyRing)
	valAccInfos := queryAccountInfo(testApp, valAccountNames, valKeyRing)

	valAccounts := CreateAccounts(valAccInfos, valAccountNames)

	// Create a signer with keyring accounts
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, app.DefaultInitialVersion, accounts...)
	require.NoError(t, err)

	valSigner, err := user.NewSigner(valKeyRing, enc.TxConfig, testutil.ChainID, app.DefaultInitialVersion, valAccounts...)
	require.NoError(t, err)

	amount := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(1_000)))

	depositAmount := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(10000000000)))

	twoInt := sdk.NewInt(2)

	// ----------- Create SDK Messages ------------

	// ---------------- First Block ------------
	var firstBlockSdkMsgs []sdk.Msg

	// Send funds to another account
	sendFundsMsg := banktypes.NewMsgSend(accountAddresses[0], accountAddresses[1], amount)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, sendFundsMsg)

	// MultiSend funds to another account
	multiSendFundsMsg := banktypes.NewMsgMultiSend([]banktypes.Input{
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
		})
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, multiSendFundsMsg)

	var grantExpiration = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	// Create a new MsgGrant
	authorization := authz.NewGenericAuthorization(blobtypes.URLMsgPayForBlobs)
	msgGrant, err := authz.NewMsgGrant(
		accountAddresses[0],
		accountAddresses[1],
		authorization,
		&grantExpiration,
	)
	require.NoError(t, err)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgGrant)

	// Create a new MsgVerifyInvariant
	msgVerifyInvariant := crisisTypes.NewMsgVerifyInvariant(accountAddresses[0], banktypes.ModuleName, "nonnegative-outstanding")
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgVerifyInvariant)

	// Create a new MsgGrantAllowance
	basicAllowance := feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(1000))),
	}
	feegrantMsg, err := feegrant.NewMsgGrantAllowance(&basicAllowance, accountAddresses[0], accountAddresses[1])
	require.NoError(t, err)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, feegrantMsg)

	// Create a new MsgSubmitProposal
	govAccount := testApp.GovKeeper.GetGovernanceAccount(testApp.NewContext(false, tmproto.Header{})).GetAddress()
	msgSend := banktypes.MsgSend{
		FromAddress: govAccount.String(),
		ToAddress:   accountAddresses[1].String(),
		Amount:      amount,
	}
	proposal, err := govtypes.NewMsgSubmitProposal([]sdk.Msg{&msgSend}, amount, accountAddresses[0].String(), "")
	require.NoError(t, err)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, proposal)

	msgDeposit := govtypes.NewMsgDeposit(accountAddresses[0], 1, depositAmount)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgDeposit)

	// Create a new MsgCreateValidator
	msgCreateValidator, err := stakingtypes.NewMsgCreateValidator(sdk.ValAddress(accountAddresses[6]),
		ed25519.GenPrivKeyFromSecret([]byte("validator")).PubKey(),
		amount[0],
		stakingtypes.NewDescription("taco tuesday", "my keybase", "www.celestia.org", "ping @celestiaorg on twitter", "fake validator"),
		stakingtypes.NewCommissionRates(sdk.NewDecWithPrec(6, 0o2), sdk.NewDecWithPrec(12, 0o2), sdk.NewDecWithPrec(1, 0o2)),
		sdk.OneInt())
	require.NoError(t, err)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgCreateValidator)

	// Create a new MsgDelegate
	msgDelegate := stakingtypes.NewMsgDelegate(accountAddresses[0], genValidators[0].GetOperator(), amount[0])
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgDelegate)

	// Create a new MsgBeginRedelegate
	msgBeginRedelegate := stakingtypes.NewMsgBeginRedelegate(accountAddresses[0], genValidators[0].GetOperator(), genValidators[1].GetOperator(), amount[0])
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgBeginRedelegate)

	// ------------ Second Block ------------

	var secondBlockSdkMsgs []sdk.Msg

	// Create a new MsgUnjail
	msgUnjail := slashingtypes.NewMsgUnjail(genValidators[3].GetOperator())
	// sdkMessages = append(sdkMessages, msgUnjail)

	// Create a new MsgVote
	msgVote := govtypes.NewMsgVote(accountAddresses[0], 1, govtypes.VoteOption_VOTE_OPTION_YES, "")
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgVote)

	// Create a new MsgRevoke
	msgRevoke := authz.NewMsgRevoke(
		accountAddresses[0],
		accountAddresses[1],
		blobtypes.URLMsgPayForBlobs,
	)

	// Create a new MsgExec to execute the revoke message
	msgExec := authz.NewMsgExec(accountAddresses[0], []sdk.Msg{&msgRevoke})
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, &msgExec)

	// Create a new MsgVoteWeighted
	msgVoteWeighted := govtypes.NewMsgVoteWeighted(
		accountAddresses[0],
		1,
		govtypes.WeightedVoteOptions([]*govtypes.WeightedVoteOption{{Option: govtypes.OptionYes, Weight: "1.0"}}), // Cast the slice to the expected type
		"",
	)
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgVoteWeighted)

	// Create a new MsgEditValidator
	msgEditValidator := stakingtypes.NewMsgEditValidator(sdk.ValAddress(accountAddresses[6]), stakingtypes.NewDescription("add", "new", "val", "desc", "."), nil, &twoInt)
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgEditValidator)

	// Create a new MsgUndelegate
	msgUndelegate := stakingtypes.NewMsgUndelegate(accountAddresses[0], genValidators[1].GetOperator(), amount[0])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgUndelegate)

	// Create a new MsgDelegate
	msgDelegate = stakingtypes.NewMsgDelegate(accountAddresses[0], genValidators[0].GetOperator(), amount[0])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgDelegate)

	// Create a new MsgCancelUnboundingDelegation
	// Messages are split in two blocks, this tx is part of the second block therefore the block height is incremented by 2
	blockHeight := testApp.LastBlockHeight() + 2
	msgCancelUnbondingDelegation := stakingtypes.NewMsgCancelUnbondingDelegation(accountAddresses[0], genValidators[1].GetOperator(), blockHeight, amount[0])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgCancelUnbondingDelegation)

	// Create a new MsgSetWithdrawAddress
	msgSetWithdrawAddress := distribution.NewMsgSetWithdrawAddress(accountAddresses[0], accountAddresses[1])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgSetWithdrawAddress)

	// Create a new MsgRevokeAllowance
	msgRevokeAllowance := feegrant.NewMsgRevokeAllowance(accountAddresses[0], accountAddresses[1])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, &msgRevokeAllowance)

	// Create a new MsgFundCommunityPool
	msgFundCommunityPool := distribution.NewMsgFundCommunityPool(amount, accountAddresses[0])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgFundCommunityPool)

	// Create a new MsgWithdrawDelegatorReward
	msgWithdrawDelegatorReward := distribution.NewMsgWithdrawDelegatorReward(accountAddresses[0], genValidators[0].GetOperator())
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgWithdrawDelegatorReward)

	// Create a new MsgWithdrawValidatorCommission
	require.NoError(t, err)
	msgWithdrawValidatorCommission := distribution.NewMsgWithdrawValidatorCommission(genValidators[0].GetOperator())

	// ------------ Construct Txs ------------
	// Create transactions from the list of messages
	firstBlockRawTxs := processSdkMessages(t, signer, firstBlockSdkMsgs)
	secondBlockRawTxs := processSdkMessages(t, signer, secondBlockSdkMsgs)
	validatorRawTxs := processSdkMessages(t, valSigner, []sdk.Msg{msgUnjail, msgWithdrawValidatorCommission})
	// Create a Blob Tx
	blobTx := BlobTx{
		author:    accountNames[1],
		blobs:     []*blob.Blob{blob.New(fixedNamespace(), []byte{1}, appconsts.DefaultShareVersion)},
		txOptions: blobfactory.DefaultTxOpts(),
	}

	rawBlobTx, _, err := signer.CreatePayForBlobs(blobTx.author, blobTx.blobs, blobTx.txOptions...)
	require.NoError(t, err)

	// validators to abci validators
	abciValidators := make([]abci.Validator, 0, len(genValidators))
	for _, val := range genValidators {
		consAddr, err := val.GetConsAddr()
		require.NoError(t, err)
		abciValidators = append(abciValidators, abci.Validator{
			Address: consAddr,
			Power:   100,
		})
	}

	_, firstBlockCommitHash := executeTxs(t, testApp, []byte{}, firstBlockRawTxs, abciValidators, testApp.LastCommitID().Hash)

	// Execute the second block
	_, secondAppHash := executeTxs(t, testApp, rawBlobTx, secondBlockRawTxs, abciValidators, firstBlockCommitHash)

	_, finalAppHash := executeTxs(t, testApp, []byte{}, validatorRawTxs, abciValidators, secondAppHash)

	fmt.Println(finalAppHash, "APP HASH")

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

func processSdkMessages(t *testing.T, signer *user.Signer, sdkMessages []sdk.Msg) [][]byte {
	rawSdkTxs := make([][]byte, 0, len(sdkMessages))
	for _, msg := range sdkMessages {
		rawSdkTx, err := signer.CreateTx([]sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
		require.NoError(t, err)
		signerAddress := msg.GetSigners()[0]
		signerAccount := signer.AccountByAddress(signerAddress)
		signer.SetSequence(signerAccount.Name(), signerAccount.Sequence()+1)
		rawSdkTxs = append(rawSdkTxs, rawSdkTx)
	}
	return rawSdkTxs
}

// Helper function to check if BlobTx is "empty"
func isBlobTxEmpty(tx BlobTx) bool {
	return tx.author == "" && (tx.blobs == nil || len(tx.blobs) == 0) && len(tx.txOptions) == 0
}

func executeTxs(t *testing.T, testApp *app.App, rawBlobTx []byte, rawSdkTxs [][]byte, validators []abci.Validator, lastCommitHash []byte) ([]byte, []byte) {
	height := testApp.LastBlockHeight() + 1
	chainId := testApp.GetChainID()

	genesisTime := util.GenesisTime

	resPrePareProposal := testApp.PrepareProposal(abci.RequestPrepareProposal{
		BlockData: &tmproto.Data{
			Txs: rawSdkTxs,
		},
		ChainId: chainId,
		Height:  height,
		Time:    genesisTime.Add(time.Duration(height) * time.Minute),
	})

	dataHash := resPrePareProposal.BlockData.Hash

	resProcessProposal := testApp.ProcessProposal(abci.RequestProcessProposal{
		BlockData: resPrePareProposal.BlockData,
		Header: core.Header{
			DataHash: resPrePareProposal.BlockData.Hash,
			ChainID:  chainId,
			Time:     genesisTime.Add(time.Duration(height) * time.Minute),
			Version:  version.Consensus{App: testApp.AppVersion()},
			Height:   height,
		},
	},
	)

	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resProcessProposal.Result, "ProcessProposal failed: ", resProcessProposal.Result)

	// BeginBlock
	header := tmproto.Header{
		Version:        version.Consensus{App: 1},
		ChainID:        chainId,
		Height:         height,
		Time:           genesisTime.Add(time.Duration(height) * time.Minute),
		LastCommitHash: lastCommitHash,
	}

	validator3Signed := height == 2 // Validator 3 signs only the first block

	// Begin block
	testApp.BeginBlock(abci.RequestBeginBlock{Header: header,
		LastCommitInfo: abci.LastCommitInfo{
			Votes: []abci.VoteInfo{
				{
					Validator:       validators[0],
					SignedLastBlock: true,
				},
				{
					Validator:       validators[3],
					SignedLastBlock: validator3Signed,
				},
			},
		},
	})

	for i, rawSdkTx := range rawSdkTxs {
		resp := testApp.DeliverTx(abci.RequestDeliverTx{Tx: rawSdkTx})
		require.Equal(t, uint32(0), resp.Code, "DeliverTx failed for the message at index %d: %s", i, resp.Log)
	}

	if len(rawBlobTx) != 0 {
		// Deliver Blob Tx
		blob, isBlobTx := blob.UnmarshalBlobTx(rawBlobTx)
		require.True(t, isBlobTx, "Not a valid BlobTx")

		respDeliverTx := testApp.DeliverTx(abci.RequestDeliverTx{Tx: blob.Tx})
		require.Equal(t, uint32(0), respDeliverTx.Code, "DeliverTx failed for the BlobTx: ", respDeliverTx.Log)
	}

	// EndBlock
	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})

	// Commit the state
	testApp.Commit()

	// Get the app hash
	appHash := testApp.LastCommitID().Hash

	return dataHash, appHash
}

func CreateAccounts(accountInfos []blobfactory.AccountInfo, accountNames []string) []*user.Account {
	accounts := make([]*user.Account, 0, len(accountInfos))
	for i, accountInfo := range accountInfos {
		account := user.NewAccount(accountNames[i], accountInfo.AccountNum, accountInfo.Sequence)
		accounts = append(accounts, account)
	}
	return accounts
}
