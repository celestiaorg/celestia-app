package testnode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRandomAccounts(t *testing.T) {
	got := RandomAccounts(2)
	assert.Len(t, got, 2)
	assert.NotEqual(t, got[0], got[1])
}
