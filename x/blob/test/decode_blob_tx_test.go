package test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/blob"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
)

type BlockResponse struct {
	Block Block `json:"block"`
}

type Block struct {
	Data BlockData `json:"data"`
}

type BlockData struct {
	Txs []string `json:"txs"`
}

// TestDecodeBlobTx demonstrates how one can take the response from the Comet
// BFT API endpoint /cosmos/base/tendermint/v1beta1/blocks/{block_number} and
// convert a base64 encoded transaction into an sdk.Tx.
//
// NOTE: this process differs from other Cosmos SDK chains because the
// transactions of type BlobTx won't be usable directly. One needs to extract
// the Tx field inside the BlobTx prior to decoding it as an sdk.Tx.
func TestDecodeBlobTx(t *testing.T) {
	blockResponse := getBlockResponse(t)

	for i, rawTx := range blockResponse.Block.Data.Txs {
		decoded, err := base64Decode(rawTx)
		if err != nil {
			t.Errorf("error decoding base64 tx: %v", err)
		}

		txBytes := getTxBytes(decoded)
		tx, err := decodeTx(txBytes)
		if err != nil {
			t.Errorf("error decoding tx: %v", err)
		}

		if i == 273 {
			// https://celenium.io/tx/C55BDD3DF3348A9F8D9206528051804754F009A1B9D0F69CCC2F9D4334215D21
			wantHash := "C55BDD3DF3348A9F8D9206528051804754F009A1B9D0F69CCC2F9D4334215D21"
			gotHash := strings.ToUpper(hex.EncodeToString(sum(txBytes)))
			assert.Equal(t, wantHash, gotHash)

			wantSigner := "celestia18y3ydyn7uslhuxu4lcm2x83gkdhrrcyaqvg6gk"
			gotSigner := tx.GetMsgs()[0].GetSigners()[0].String()
			assert.Equal(t, gotSigner, wantSigner)
		}
	}
}

func getBlockResponse(t *testing.T) BlockResponse {
	// block_response.json is the JSON response from the API endpoint:
	// https://api.celestia.pops.one/cosmos/base/tendermint/v1beta1/blocks/408
	// The response was persisted to block_response.json so that this test
	// doesn't execute an HTTP request every invocation.
	path := filepath.Join("testdata", "block_response.json")
	fileContents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading json file: %v", err)
	}

	var blockResponse BlockResponse
	if err := json.Unmarshal(fileContents, &blockResponse); err != nil {
		t.Fatalf("error decoding JSON block response: %v", err)
	}
	return blockResponse
}

func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func getTxBytes(txBytes []byte) []byte {
	bTx, isBlob := blob.UnmarshalBlobTx(txBytes)
	if isBlob {
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

func sum(bz []byte) []byte {
	h := sha256.Sum256(bz)
	return h[:]
}
