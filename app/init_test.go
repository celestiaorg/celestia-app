package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_getDefaultNodeHome(t *testing.T) {
	type testCase struct {
		name         string
		celestiaHome string
		userHome     string
		want         string
	}

	testCases := []testCase{
		{
			name:         "by default use the root directory",
			celestiaHome: "",
			userHome:     "",
			want:         ".celestia-app",
		},
		{
			name:         "use celestia-home if it is set",
			celestiaHome: "celestia-home",
			userHome:     "",
			want:         "celestia-home/.celestia-app",
		},
		{
			name:         "use user-home if it is set",
			celestiaHome: "",
			userHome:     "user-home",
			want:         "user-home/.celestia-app",
		},
		{
			name:         "celestia-home takes precedence over user-home",
			celestiaHome: "celestia-home",
			userHome:     "user-home",
			want:         "celestia-home/.celestia-app",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := getDefaultNodeHome(tc.celestiaHome, tc.userHome)
			assert.Equal(t, tc.want, got)
		})
	}
}
