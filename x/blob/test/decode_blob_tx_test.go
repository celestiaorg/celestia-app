package test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/tx"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/tendermint/tendermint/proto/tendermint/blockchain"
)

// TestDecodeBlobTx demonstrates how one can take the response from the
// celestia-core API endpoint:
// `/cosmos/base/tendermint/v1beta1/blocks/{block_number}`
// and convert all encoded transactions into sdk.Tx.
//
// NOTE: this process differs from other Cosmos SDK chains because the
// transactions of type BlobTx won't be usable directly. One needs to extract
// the Tx field inside the BlobTx prior to decoding it as an sdk.Tx.
func TestDecodeBlobTx(t *testing.T) {
	blockResponse := getTestdataBlockResponse(t)

	for i, rawTx := range blockResponse.Block.Data.Txs {
		txBytes := getTxBytes(rawTx)
		tx, err := decodeTx(txBytes)
		if err != nil {
			t.Errorf("error decoding tx: %v", err)
		}

		// The last transaction in the block is a blob transaction.
		// https://celenium.io/tx/C55BDD3DF3348A9F8D9206528051804754F009A1B9D0F69CCC2F9D4334215D21
		if i == 273 {
			wantHash := "C55BDD3DF3348A9F8D9206528051804754F009A1B9D0F69CCC2F9D4334215D21"
			gotHash := strings.ToUpper(hex.EncodeToString(hash(txBytes)))
			assert.Equal(t, wantHash, gotHash)

			msg := tx.GetMsgs()[0]
			wantSigner := "celestia18y3ydyn7uslhuxu4lcm2x83gkdhrrcyaqvg6gk"
			gotSigner := msg.GetSigners()[0].String()
			assert.Equal(t, gotSigner, wantSigner)

			msgPayForBlobs, ok := msg.(*blobtypes.MsgPayForBlobs)
			if !ok {
				t.Errorf("expected MsgPayForBlobs, got %T", msg)
			}
			wantNamespace := []byte{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x8, 0xe5, 0xf6, 0x79, 0xbf, 0x71, 0x16, 0xcb}
			gotNamespace := msgPayForBlobs.Namespaces[0]
			assert.Equal(t, wantNamespace, gotNamespace)
		}
	}
}

// getTestdataBlockResponse gets the block response from the testdata directory.
func getTestdataBlockResponse(t *testing.T) (resp blockchain.BlockResponse) {
	// block_response.json is the JSON response from the API endpoint:
	// https://api.celestia.pops.one/cosmos/base/tendermint/v1beta1/blocks/408
	// The response was persisted to block_response.json so that this test
	// doesn't execute an HTTP request every invocation.
	path := filepath.Join("testdata", "block_response.json")
	fileContents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading json file: %v", err)
	}

	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	if err = encodingConfig.Codec.UnmarshalJSON(fileContents, &resp); err != nil {
		t.Fatalf("error unmarshal JSON block response: %v", err)
	}
	return resp
}

func getTxBytes(txBytes []byte) []byte {
	bTx, isBlob, err := tx.UnmarshalBlobTx(txBytes)
	if isBlob {
		if err != nil {
			panic(err)
		}
		return bTx.Tx
	}
	return txBytes
}

func decodeTx(txBytes []byte) (types.Tx, error) {
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decoder := encodingConfig.TxConfig.TxDecoder()
	tx, err := decoder(txBytes)
	if err != nil {
		return nil, fmt.Errorf("error decoding transaction: %v", err)
	}
	return tx, nil
}

func hash(bz []byte) []byte {
	checksum := sha256.Sum256(bz)
	return checksum[:]
}
