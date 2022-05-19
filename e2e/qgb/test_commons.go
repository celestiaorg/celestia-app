package e2e

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func HandleNetworkError(t *testing.T, network *QGBNetwork, err error) {
	if err != nil {
		network.PrintLogs()
		assert.NoError(t, err)
		t.FailNow()
	}
}
