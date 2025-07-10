//go:build multiplexer

package embedding

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCelestiaAppV3(t *testing.T) {
	// prevent messing with other tests by modifying this.
	realData := v3binaryCompressed
	defer func() {
		v3binaryCompressed = realData
	}()

	testCases := []struct {
		name            string
		modifyFn        func()
		expectedVersion string
		expectedError   error
	}{
		{
			name: "valid binary data",
			modifyFn: func() {
				v3binaryCompressed = realData
			},
			expectedVersion: v3Version,
		},
		{
			name: "nil binaryCompressed",
			modifyFn: func() {
				v3binaryCompressed = nil
			},
			expectedError: fmt.Errorf("no binary data available for platform %s", platform()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.modifyFn()
			version, binary, err := CelestiaAppV3()

			if tc.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tc.expectedError, err)
				assert.Empty(t, version)
				assert.Nil(t, binary)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedVersion, version)
				assert.NotEmpty(t, binary)
			}
		})
	}
}
