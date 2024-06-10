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
	"github.com/celestiaorg/go-square/blob"
	appns "github.com/celestiaorg/go-square/namespace"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
)

type sdkTx struct {
	sdkMsgs   []sdk.Msg
	txOptions []user.TxOption
}

type blobTx struct {
	author    string
	blobs     []*blob.Blob
	txOptions []user.TxOption
}

// TestConsistentAppHash executes a set of txs, generates an app hash,
// and compares it against a previously generated hash from the same set of transactions.
// App hashes across different commits should be consistent.
func TestConsistentAppHash(t *testing.T) {
	// Expected app hash produced by v1.x - TODO: link to the test producing the hash
	expectedAppHash := []byte{9, 208, 117, 101, 108, 61, 146, 58, 26, 190, 199, 124, 76, 178, 84, 74, 54, 159, 76, 187, 2, 169, 128, 87, 70, 78, 8, 192, 28, 144, 116, 117}

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
	_, _, err = testutil.ApplyGenesisState(testApp, pubKeys, 1_000_000_000, app.DefaultInitialConsensusParams())
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

	// Create an SDK Tx
	sdkTx := sdkTx{
		sdkMsgs: []sdk.Msg{
			banktypes.NewMsgSend(signer.Account(accountNames[0]).Address(),
				signer.Account(accountNames[1]).Address(),
				amount),
		},
		txOptions: blobfactory.DefaultTxOpts(),
	}

	// Create a Blob Tx
	blobTx := blobTx{
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

	// Require that the app hash is equal to the app hash produced on a different commit
	require.Equal(t, expectedAppHash, appHash)
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
