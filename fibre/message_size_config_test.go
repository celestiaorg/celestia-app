package fibre

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidateMaxMessageSize(t *testing.T) {
	tooBig := int64(math.MaxInt32) + 1
	for _, size := range []int{-1, 0, 1, math.MaxInt32, int(tooBig)} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			client := DefaultClientConfig()
			client.StateAddress = "localhost:9090"
			client.MaxMessageSize = size
			server := DefaultServerConfig()
			server.Path = t.TempDir()
			server.MaxMessageSize = size
			for _, validate := range []func() error{client.Validate, server.Validate} {
				err := validate()
				if size > 0 && int64(size) <= math.MaxInt32 {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "MaxMessageSize")
				}
			}
		})
	}
}
