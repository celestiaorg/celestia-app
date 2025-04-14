package testdata

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCelestiaApp(t *testing.T) {
	// prevent messing with other tests by modifying this.
	realData := binaryCompressed
	defer func() {
		binaryCompressed = realData
	}()

	tests := []struct {
		name          string
		modifyFn      func()
		expectedError error
	}{
		{
			name: "Valid binary data",
			modifyFn: func() {
				binaryCompressed = realData
			},
			expectedError: nil,
		},
		{
			name: "nil binaryCompressed",
			modifyFn: func() {
				binaryCompressed = nil
			},
			expectedError: fmt.Errorf("no binary data available for platform %s", platform()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			tt.modifyFn()

			data, err := CelestiaApp()

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
