package shares

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/assert"
)

func TestIsEmptyShare(t *testing.T) {
	namespaceOne := bytes.Repeat([]byte{0x01}, appconsts.NamespaceSize)
	b := NewBuilder(namespaceOne, 0, false)

	got := b.IsEmptyShare()
	assert.Equal(t, true, got)
}
