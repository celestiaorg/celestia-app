package shares

import "errors"

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

	seqStart, err := shares[0].IsSequenceStart()
	if err != nil {
		return nil, err
	}
	if !seqStart {
		return nil, errors.New("first share is not the start of a sequence")
	}

	err = validateShareVersions(shares, supportedShareVersions)
	if err != nil {
		return nil, err
	}

	rawData, err := extractRawData(shares)
	if err != nil {
		return nil, err
	}

	data, err = parseRawData(rawData)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// validateShareVersions returns an error if the shares contain a share with an
// unsupported share version. Returns nil if all shares contain supported share
// versions.
func validateShareVersions(shares []Share, supportedShareVersions []uint8) error {
	for i := 0; i < len(shares); i++ {
		if err := shares[i].DoesSupportVersions(supportedShareVersions); err != nil {
			return err
		}
	}
	return nil
}

// parseRawData returns the units (transactions, PFB transactions, intermediate
// state roots) contained in raw data by parsing the unit length delimiter
// prefixed to each unit.
func parseRawData(rawData []byte) (units [][]byte, err error) {
	units = make([][]byte, 0)
	for {
		actualData, unitLen, err := ParseDelimiter(rawData)
		if err != nil {
			return nil, err
		}
		if unitLen == 0 {
			return units, nil
		}
		rawData = actualData[unitLen:]
		units = append(units, actualData[:unitLen])
	}
}

// extractRawData returns the raw data contained in the shares. The raw data does
// not contain the namespace ID, info byte, sequence length, or reserved bytes.
func extractRawData(shares []Share) (rawData []byte, err error) {
	for i := 0; i < len(shares); i++ {
		raw, err := shares[i].RawData()
		if err != nil {
			return nil, err
		}
		rawData = append(rawData, raw...)
	}
	return rawData, nil
}
