package shares

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
		// the rest of raw data is padding
		if unitLen == 0 {
			return units, nil
		}
		// the rest of actual data contains only part of the next transaction so
		// we stop parsing raw data
		if unitLen > uint64(len(actualData)) {
			return units, nil
		}
		rawData = actualData[unitLen:]
		units = append(units, actualData[:unitLen])
	}
}

// extractRawData returns the raw data representing complete transactions
// contained in the shares. The raw data does not contain the namespace, info
// byte, sequence length, or reserved bytes. Starts reading raw data based on
// the reserved bytes in the first share.
func extractRawData(shares []Share) (rawData []byte, err error) {
	for i := 0; i < len(shares); i++ {
		var raw []byte
		if i == 0 {
			raw, err = shares[i].RawDataUsingReserved()
		} else {
			raw, err = shares[i].RawData()
		}
		if err != nil {
			return nil, err
		}
		rawData = append(rawData, raw...)
	}
	return rawData, nil
}
