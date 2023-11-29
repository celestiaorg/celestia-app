package cli

import (
	"encoding/json"
	"os"

	"github.com/celestiaorg/celestia-app/pkg/blob"
	"github.com/cosmos/cosmos-sdk/codec"
)

// Define the raw content from the file input.
type blobs struct {
	Blobs []json.RawMessage
}

func parseSubmitBlobs(cdc codec.Codec, path string) ([]blob.BlobJson, error) {
	var rawBlobs blobs

	content, err := os.ReadFile(path)
	if err != nil {
		return []blob.BlobJson{}, err
	}

	err = json.Unmarshal(content, &rawBlobs)
	if err != nil {
		return []blob.BlobJson{}, err
	}

	blobs := make([]blob.BlobJson, len(rawBlobs.Blobs))
	for i, anyJSON := range rawBlobs.Blobs {
		var blob blob.BlobJson
		err = cdc.UnmarshalJSON(anyJSON, &blob)
		if err != nil {
			break
		}

		blobs[i] = blob
	}

	return blobs, err
}
