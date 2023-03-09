package shares

import (
	"bytes"
	"errors"
	"fmt"
)

// parseCompactShares returns data (transactions or intermediate state roots
// based on the contents of rawShares and supportedShareVersions. If rawShares
// contains a share with a version that isn't present in supportedShareVersions,
// an error is returned. The returned data [][]byte does not have namespaces,
// info bytes, data length delimiter, or unit length delimiters and are ready to
// be unmarshalled.
func parseCompactShares(shares []Share, supportedShareVersions []uint8) (data [][]byte, err error) {
	if len(shares) == 0 {
		return nil, nil
	}
	for _, share := range shares {
		infoByte, err := share.InfoByte()
		if err != nil {
			return nil, err
		}
		if !bytes.Contains(supportedShareVersions, []byte{infoByte.Version()}) {
			return nil, fmt.Errorf("unsupported share version %v is not present in the list of supported share versions %v", infoByte.Version(), supportedShareVersions)
		}
	}

	return peel(shares)
}

// peel parses each unit of data (either a transaction or
// intermediate state root) and adds it to the underlying slice of data.
// data may be transactions or intermediate state roots depending
// on the namespace ID for this share
func peel(shares []Share) (data [][]byte, err error) {

	seqStart, err := shares[0].IsSequenceStart()
	if err != nil {
		return nil, err
	}
	if !seqStart {
		return nil, errors.New("first share is not the start of a sequence")
	}

	data = make([][]byte, 0) // not sure if we really need this

	allRawBytes := make([]byte, 0)
	for i := 0; i < len(shares); i++ {

		rawData, err := shares[i].RawData()
		if err != nil {
			return nil, err
		}
		allRawBytes = append(allRawBytes, rawData...)
	}

	for {
		actualData, unitLen, err := ParseDelimiter(allRawBytes)
		if err != nil {
			return nil, err
		}
		if unitLen == 0 {
			return data, nil
		}
		allRawBytes = actualData[unitLen:]
		data = append(data, actualData[:unitLen])
	}
}
