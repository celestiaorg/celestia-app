package types

var (
	// UpgradeKey is the key in the signal store used to persist an upgrade if one is
	// pending.
	UpgradeKey = []byte{0x00}

	// FirstSignalKey is used as a divider to separate the UpgradeKey from all
	// the keys associated with signals from validators. In practice, this key
	// isn't expected to be set or retrieved. It must be lexicographically
	// greater than UpgradeKey (1 byte) but less than any 20-byte validator address.
	FirstSignalKey = []byte{0x00, 0x00}
)
