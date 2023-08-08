package v1

const (
	Version              uint64 = 1
	SquareSizeUpperBound int    = 128
	SubtreeRootThreshold int    = 64
	MaxTotalBlobSize     int    = firstSparseShareContentSize + continuationSparseShareContentSize*(defaultGovMaxSquareSize*defaultGovMaxSquareSize-2) // 1_932_846 bytes

	// The following constants are redefined in this file because they
	// can't be imported from appconsts without introducing a circular
	// dependency.
	firstSparseShareContentSize        int = 478
	continuationSparseShareContentSize int = 472
	defaultGovMaxSquareSize            int = 64
)
