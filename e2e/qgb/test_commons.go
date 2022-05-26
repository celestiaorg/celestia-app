package e2e

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

const TRUE = "true"

func HandleNetworkError(t *testing.T, network *QGBNetwork, err error, expectError bool) {
	if expectError && err == nil {
		network.PrintLogs()
		assert.Error(t, err)
		t.FailNow()
	} else if !expectError && err != nil {
		network.PrintLogs()
		assert.NoError(t, err)
		t.FailNow()
	}
}
