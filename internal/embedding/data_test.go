//go:build multiplexer

package embedding

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			expectedVersion: "v3.10.0-arabica",
			expectedError:   nil,
		},
		{
			name: "nil binaryCompressed",
			modifyFn: func() {
				v3binaryCompressed = nil
			},
			expectedError: fmt.Errorf("no binary data available for platform %s", platform()),
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			tt.modifyFn()

			data, version, err := CelestiaAppV3()

			if tt.expectedError != nil {
				require.Error(t, err)
				require.Equal(t, err, tt.expectedError)
				require.Nil(t, data)
			} else {
				require.NoError(t, err)
				require.NotNil(t, data)
				require.NotEmpty(t, data)
				assert.Equal(t, tt.expectedVersion, version)
			}
		})
	}
}

func TestGetVersion(t *testing.T) {
	want := "v3.10.0-arabica"
	got, err := getVersion()
	assert.NoError(t, err)
	assert.Equal(t, want, got)
}
