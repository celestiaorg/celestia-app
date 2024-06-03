package app_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	"github.com/celestiaorg/go-square/blob"
	appns "github.com/celestiaorg/go-square/namespace"
	"github.com/cosmos/cosmos-sdk/codec"
	hd "github.com/cosmos/cosmos-sdk/crypto/hd"
	keyring "github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

// TestNonDeterminismBetweenMainAndV1 executes a set of different transactions,
// produces an app hash and compares it with the app hash produced by v1.x
func TestNonDeterminismBetweenMainAndV1(t *testing.T) {
	const (
		numBlobTxs, numNormalTxs = 5, 5
	)

	expectedAppHash := []byte{100, 237, 125, 126, 116, 10, 189, 82, 156, 116, 176, 136, 169, 92, 185, 12, 72, 134, 254, 175, 234, 13, 159, 90, 139, 192, 190, 248, 67, 9, 32, 217}

	// Initialize testApp
	testApp := testutil.NewTestApp()

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	// Create deterministic keys
	kr, pubKeys := DeterministicKeyRing(enc.Codec)

	recs, err := kr.List()
	require.NoError(t, err)
	addresses := make([]string, 0, len(recs))

	// Get the name of the records
	for _, rec := range recs {
		addresses = append(addresses, rec.Name)
	}

	// Apply genesis state to the app
	_, _, err = testutil.ApplyGenesisState(testApp, pubKeys, 1_000_000_000, app.DefaultInitialConsensusParams())
	require.NoError(t, err)

	accinfos := queryAccountInfo(testApp, addresses, kr)

	// Create a set of 10 deterministic sdk transactions
	normalTxs := testutil.SendTxsWithAccounts(
		t,
		testApp,
		enc.TxConfig,
		kr,
		1000,
		addresses[0],
		addresses[:numNormalTxs],
		testutil.ChainID,
	)

	// Create a set of 5 deterministic blob transactions
	blobTxs := blobfactory.ManyMultiBlobTx(t, enc.TxConfig, kr, testutil.ChainID, addresses[numBlobTxs+1:], accinfos[numBlobTxs+1:], testfactory.Repeat([]*blob.Blob{
		blob.New(DeterministicNamespace(), []byte{1}, appconsts.DefaultShareVersion),
	}, numBlobTxs), app.DefaultInitialConsensusParams().Version.AppVersion)

	// Deliver sdk txs
	for _, tx := range normalTxs {
		resp := testApp.DeliverTx(abci.RequestDeliverTx{Tx: tx})
		require.EqualValues(t, 0, resp.Code, resp.Log)
	}

	// Deliver blob txs
	for _, tx := range blobTxs {
		blobTx, ok := blob.UnmarshalBlobTx(tx)
		require.True(t, ok)
		resp := testApp.DeliverTx(abci.RequestDeliverTx{Tx: blobTx.Tx})
		require.EqualValues(t, 0, resp.Code, resp.Log)
	}

	// Commit the state
	testApp.Commit()

	// Get the app hash
	appHash := testApp.LastCommitID().Hash

	// Assert that the app hash is equal to the app hash produced by v1.x
	require.Equal(t, expectedAppHash, appHash)
}

// DeterministicNamespace returns a deterministic namespace
func DeterministicNamespace() appns.Namespace {
	return appns.Namespace{
		Version: 0,
		ID:      []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 37, 67, 154, 200, 228, 130, 74, 147, 162, 11},
	}
}

// DeterministicKeyRing returns a deterministic keyring and a list of deterministic public keys
func DeterministicKeyRing(cdc codec.Codec) (keyring.Keyring, []types.PubKey) {
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
