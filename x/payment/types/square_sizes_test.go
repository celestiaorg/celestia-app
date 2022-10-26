package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllSquareSizes(t *testing.T) {
	got := AllSquareSizes()
	expected := []uint64{8, 16, 32, 64, 128}
	assert.Equal(t, expected, got)
}
