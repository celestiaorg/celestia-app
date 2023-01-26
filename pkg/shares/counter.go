package shares

import "github.com/celestiaorg/celestia-app/pkg/appconsts"

type CompactShareCounter struct {
	lastShares    int
	lastRemainder int
	shares        int
	remainder     int
}

func NewCounter() *CompactShareCounter {
	return &CompactShareCounter{}
}

func (c *CompactShareCounter) Add(dataLen int) int {
	dataLen += DelimLen(uint64(dataLen))
	c.lastRemainder = c.remainder
	c.lastShares = c.shares
	if c.shares == 0 {
		if dataLen+c.remainder > appconsts.FirstCompactShareContentSize {
			dataLen -= (appconsts.FirstCompactShareContentSize - c.remainder)
			c.shares++
			c.remainder = 0
		} else {
			c.remainder += dataLen
			dataLen = 0
		}
	}
	if dataLen > (appconsts.ContinuationCompactShareContentSize - c.remainder) {
		dataLen -= (appconsts.ContinuationCompactShareContentSize - c.remainder)
		c.shares++
		c.remainder = 0
	} else {
		c.remainder += dataLen
		dataLen = 0
	}
	if dataLen > 0 {
		c.shares += dataLen / appconsts.ContinuationCompactShareContentSize
		c.remainder = dataLen % appconsts.ContinuationCompactShareContentSize
	}
	diff := c.shares - c.lastShares
	if c.lastRemainder == 0 && c.remainder > 0 {
		diff++
	} else if c.lastRemainder > 0 && c.remainder == 0 {
		diff--
	}
	return diff
}

func (c *CompactShareCounter) RevertLast() {
	c.shares = c.lastShares
	c.remainder = c.lastRemainder
}

func (c *CompactShareCounter) Size() int {
	if c.remainder == 0 {
		return c.shares
	}
	return c.shares + 1
}
