//go:build multiplexer

package embedding

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCelestiaApp(t *testing.T) {
	// prevent messing with other tests by modifying this.
	realData := v3binaryCompressed
	defer func() {
		v3binaryCompressed = realData
	}()

	tests := []struct {
		name          string
		modifyFn      func()
		expectedError error
	}{
		{
			name: "valid binary data",
			modifyFn: func() {
				v3binaryCompressed = realData
			},
			expectedError: nil,
		},
		{
			name: "nil binaryCompressed",
			modifyFn: func() {
				v3binaryCompressed = nil
			},
			expectedError: fmt.Errorf("no binary data available for platform %s", platform()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.modifyFn()

			data, err := CelestiaAppV3()

			if tt.expectedError != nil {
				require.Error(t, err)
				require.Equal(t, err, tt.expectedError)
				require.Nil(t, data)
			} else {
				require.NoError(t, err)
				require.NotNil(t, data)
				require.NotEmpty(t, data)
			}
		})
	}
}
