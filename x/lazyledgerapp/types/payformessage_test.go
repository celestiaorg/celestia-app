package types

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMountainRange(t *testing.T) {
	type test struct {
		l, k     uint64
		expected []uint64
	}
	tests := []test{
		{
			l:        11,
			k:        4,
			expected: []uint64{4, 4, 2, 1},
		},
		{
			l:        2,
			k:        64,
			expected: []uint64{2},
		},
		{ //should this test throw an error? we
			l:        64,
			k:        8,
			expected: []uint64{8, 8, 8, 8, 8, 8, 8, 8},
		},
	}
	for _, tt := range tests {
		res := PowerOf2MountainRange(tt.l, tt.k)
		assert.Equal(t, tt.expected, res)
	}
}

func TestNextPowerOf2(t *testing.T) {
	type test struct {
		input    uint64
		expected uint64
	}
	tests := []test{
		{
			input:    2,
			expected: 2,
		},
		{
			input:    11,
			expected: 8,
		},
		{
			input:    511,
			expected: 256,
		},
		{
			input:    1,
			expected: 1,
		},
		{
			input:    0,
			expected: 0,
		},
	}
	for _, tt := range tests {
		res := nextPowerOf2(tt.input)
		assert.Equal(t, tt.expected, res)
	}
}

// TestCreateCommit only shows if something changed, it doesn't actually show
// the commit is being created correctly todo(evan): fix me.
func TestCreateCommitment(t *testing.T) {
	type test struct {
		k         uint64
		namespace []byte
		message   []byte
		expected  []byte
	}
	tests := []test{
		{
			k:         4,
			namespace: bytes.Repeat([]byte{0xFF}, 8),
			message:   bytes.Repeat([]byte{0xFF}, 11*256),
			expected:  []byte{0x5d, 0x43, 0xd7, 0x40, 0xe5, 0xe6, 0x5e, 0x2a, 0xb9, 0x10, 0x5c, 0xf9, 0x26, 0xf9, 0xf0, 0x1c, 0x3a, 0x11, 0x49, 0x1c, 0x71, 0x21, 0xdf, 0x46, 0xdd, 0x21, 0x94, 0x3f, 0xba, 0xb1, 0xcf, 0xd4},
		},
	}
	for _, tt := range tests {
		res, err := CreateCommitment(tt.k, tt.namespace, tt.message)
		assert.NoError(t, err)
		assert.Equal(t, tt.expected, res)
	}
}

// this test only tests for changes, it doesn't actually test that the result is valid.
// todo(evan): fixme
func TestGetCommitmentSignBytes(t *testing.T) {
	type test struct {
		msg      MsgWirePayForMessage
		expected []byte
	}
	tests := []test{
		{
			msg: MsgWirePayForMessage{
				MessageSize:        4,
				Message:            []byte{1, 2, 3, 4},
				MessageNameSpaceId: []byte{1, 2, 3, 4, 1, 2, 3, 4},
				Nonce:              1,
				Fee: &TransactionFee{
					BaseRateMax: 10000,
					TipRateMax:  1000,
				},
			},
			expected: []byte(`{"fee":{"base_rate_max":"10000","tip_rate_max":"1000"},"message_namespace_id":"AQIDBAECAwQ=","message_share_commitment":"byozRVIrw5NF/rU1PPyq6BAo3g2ny3uLTiOFedtgSwo=","message_size":"4","nonce":"1"}`),
		},
	}
	for _, tt := range tests {
		res, err := tt.msg.GetCommitmentSignBytes(SquareSize)
		assert.NoError(t, err)
		assert.Equal(t, tt.expected, res)
	}
}

func TestShareChunkingAndPadding(t *testing.T) {
	type test struct {
		input  []byte
		expect [][]byte
	}
	tests := []test{
		{
			input:  []byte{1},
			expect: [][]byte{append([]byte{1}, bytes.Repeat([]byte{0}, ShareSize-1)...)},
		},
		{
			input:  bytes.Repeat([]byte{1}, ShareSize),
			expect: [][]byte{bytes.Repeat([]byte{1}, ShareSize)},
		},
	}
	for _, tt := range tests {
		shares := chunkMessage(tt.input)
		shares = addSharePadding(shares)
		for _, share := range shares {
			if len(share) != ShareSize {
				t.Errorf("invalid share length: got %d wanted core.ShareSize (%d)", len(share), ShareSize)
			}
		}
		assert.Equal(t, tt.expect, shares)
	}
}
