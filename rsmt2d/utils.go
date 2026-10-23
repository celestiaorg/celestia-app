package rsmt2d

func flattenShares(shares [][]byte) []byte {
	length := 0
	for _, share := range shares {
		length += len(share)
	}

	flattened := make([]byte, 0, length)
	for _, share := range shares {
		flattened = append(flattened, share...)
	}

	return flattened
}
