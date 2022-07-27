package e2e

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
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
		if errors.Is(err, ErrNetworkStopped) {
			// if some other error orrured, we notify.
			network.toStopChan <- struct{}{}
		}
		t.FailNow()
	}
}
