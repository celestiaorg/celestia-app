package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cosmos/cosmos-sdk/codec"
)

type blobs struct {
	Blobs []json.RawMessage
}

type blobJSON struct {
	NamespaceID string
	Blob        string
}

func parseSubmitBlobs(cdc codec.Codec, path string) ([]blobJSON, error) {
	var rawBlobs blobs

	content, err := os.ReadFile(path)
	if err != nil {
		return []blobJSON{}, err
	}

	err = json.Unmarshal(content, &rawBlobs)
	if err != nil {
		return []blobJSON{}, err
	}

	blobs := make([]blobJSON, len(rawBlobs.Blobs))
	for i, anyJSON := range rawBlobs.Blobs {
		var blob blobJSON
		fmt.Println(anyJSON)
		err := cdc.UnmarshalJSON(anyJSON, blob)
		if err != nil {
			return []blobJSON{}, err
		}

		blobs[i] = blob
	}

	fmt.Println(blobs)

	return blobs, nil
}
