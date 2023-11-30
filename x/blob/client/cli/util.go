package cli

import (
	"encoding/json"
	"os"

	"github.com/cosmos/cosmos-sdk/codec"
)

// Define the raw content from the file input.
type blobs struct {
	Blobs []blobJson
}

type blobJson struct {
	NamespaceId string
	Blob        string
}

func parseSubmitBlobs(cdc codec.Codec, path string) ([]blobJson, error) {
	var rawBlobs blobs

	content, err := os.ReadFile(path)
	if err != nil {
		return []blobJson{}, err
	}

	err = json.Unmarshal(content, &rawBlobs)
	if err != nil {
		return []blobJson{}, err
	}

	return rawBlobs.Blobs, err
}
