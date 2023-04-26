package shares

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

type CompactShareCounter struct {
	lastShares    int
	lastRemainder int
	shares        int
	// remainder is the number of bytes used for data in the last share
	remainder int
}

// NewCompactShareCounter creates a new instance of a counter which calculates the amount
// of compact shares a set of data will be split into.
func NewCompactShareCounter() *CompactShareCounter {
	return &CompactShareCounter{}
}

// Add adds the length of the data to the counter and returns the amount of shares
// the counter has been increased by.
func (c *CompactShareCounter) Add(dataLen int) int {
	// Increment the data len by the varint that will prefix the data.
	dataLen += DelimLen(uint64(dataLen))

	// save a copy of the previous state
	c.lastRemainder = c.remainder
	c.lastShares = c.shares

	// if this is the first share, calculate how much is taken up by dataLen
	if c.shares == 0 {
		if dataLen >= appconsts.FirstCompactShareContentSize-c.remainder {
			dataLen -= (appconsts.FirstCompactShareContentSize - c.remainder)
			c.shares++
			c.remainder = 0
		} else {
			c.remainder += dataLen
			dataLen = 0
		}
	}

	// next, look to fill the remainder of the continuation share
	if dataLen >= (appconsts.ContinuationCompactShareContentSize - c.remainder) {
		dataLen -= (appconsts.ContinuationCompactShareContentSize - c.remainder)
		c.shares++
		c.remainder = 0
	} else {
		c.remainder += dataLen
		dataLen = 0
	}

	// finally, divide the remaining dataLen into the continuation shares and update
	// the remainder
	if dataLen > 0 {
		c.shares += dataLen / appconsts.ContinuationCompactShareContentSize
		c.remainder = dataLen % appconsts.ContinuationCompactShareContentSize
	}

	// calculate the diff between before and after
	diff := c.shares - c.lastShares
	if c.lastRemainder == 0 && c.remainder > 0 {
		diff++
	} else if c.lastRemainder > 0 && c.remainder == 0 {
		diff--
	}
	return diff
}

// Revert reverts the last Add operation. This can be called multiple times but only works
// the first time after an add operation.
func (c *CompactShareCounter) Revert() {
	c.shares = c.lastShares
	c.remainder = c.lastRemainder
}

// Size returns the amount of shares the compact share counter has counted.
func (c *CompactShareCounter) Size() int {
	if c.remainder == 0 {
		return c.shares
	}
	return c.shares + 1
}
