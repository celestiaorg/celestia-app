package types

var (
	// UpgradeKey is the key in the signal store used to persist a upgrade if one is
	// pending.
	UpgradeKey = []byte{0x00}

	// FirstSignalKey is used as a divider to separate the UpgradeKey from all
	// the keys associated with signals from validators. In practice, this key
	// isn't expected to be set or retrieved.
	FirstSignalKey = []byte{0x000}
)
