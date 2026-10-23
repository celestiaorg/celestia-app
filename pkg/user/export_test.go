package user

import "time"

// WithEvictionPollTimeout lets tests shorten the eviction poll timeout.
func WithEvictionPollTimeout(timeout time.Duration) Option {
	return func(c *TxClient) {
		c.evictionPollTimeout = timeout
	}
}
