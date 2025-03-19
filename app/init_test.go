package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_getDefaultNodeHome(t *testing.T) {
	type testCase struct {
		name         string
		nodeHome     string
		userHome     string
		celestiaHome string
		want         string
	}

	testCases := []testCase{
		{
			name:         "if everything is empty, use the root",
			nodeHome:     "",
			celestiaHome: "",
			userHome:     "",
			want:         ".celestia-app",
		},
		{
			name:         "if nodeHome is set, use it",
			nodeHome:     "node-home",
			userHome:     "",
			celestiaHome: "",
			want:         "node-home/.celestia-app",
		},
		{
			name:         "if celestiaHome is set, use it",
			nodeHome:     "",
			celestiaHome: "celestia-home",
			userHome:     "",
			want:         "celestia-home/.celestia-app",
		},
		{
			name:         "if userHome is set, use it",
			nodeHome:     "",
			celestiaHome: "",
			userHome:     "user-home",
			want:         "user-home/.celestia-app",
		},
		{
			name:         "node-home takes precedence over celestia-home and user-home",
			nodeHome:     "node-home",
			celestiaHome: "celestia-home",
			userHome:     "user-home",
			want:         "node-home/.celestia-app",
		},
		{
			name:         "celestia-home takes precedence over user-home",
			nodeHome:     "",
			celestiaHome: "celestia-home",
			userHome:     "user-home",
			want:         "celestia-home/.celestia-app",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := getDefaultNodeHome(tc.nodeHome, tc.celestiaHome, tc.userHome)
			assert.Equal(t, tc.want, got)
		})
	}
}
