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
	var content blobs

	rawBlobs, err := os.ReadFile(path)
	if err != nil {
		return []blobJSON{}, err
	}

	fmt.Println(rawBlobs)

	err = json.Unmarshal(rawBlobs, &content)
	if err != nil {
		return []blobJSON{}, err
	}

	fmt.Println(content)

	blobs := make([]blobJSON, len(content.Blobs))
	for i, anyJSON := range content.Blobs {
		var blob blobJSON
		err := cdc.UnmarshalInterfaceJSON(anyJSON, &blob)
		if err != nil {
			return []blobJSON{}, err
		}

		blobs[i] = blob
	}

	fmt.Println(blobs)

	return blobs, nil
}
