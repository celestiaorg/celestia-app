package cli

import (
	"encoding/json"
	"os"
)

// blobs defines the structure of the JSON file input for multiple blobs.
type blobs struct {
	Blobs []blobJSON
}

type blobJSON struct {
	NamespaceID string
	Blob        string
}

func parseSubmitBlobs(path string) ([]blobJSON, error) {
	var rawBlobs blobs

	content, err := os.ReadFile(path)
	if err != nil {
		return []blobJSON{}, err
	}

	err = json.Unmarshal(content, &rawBlobs)
	if err != nil {
		return []blobJSON{}, err
	}

	return rawBlobs.Blobs, err
}
