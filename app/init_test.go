package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_getDefaultNodeHome(t *testing.T) {
	type testCase struct {
		name         string
		userHome     string
		celestiaHome string
		want         string
	}

	testCases := []testCase{
		{
			name:         "want .celestia-app when userHome is empty and celestiaHome is empty",
			userHome:     "",
			celestiaHome: "",
			want:         ".celestia-app",
		},
		{
			name:         "want celestia-home/.celestia-app when userHome is empty and celestiaHome is not empty",
			userHome:     "",
			celestiaHome: "celestia-home",
			want:         "celestia-home/.celestia-app",
		},
		{
			name:         "want user-home/.celestia-app when userHome is not empty and celestiaHome is empty",
			userHome:     "user-home",
			celestiaHome: "",
			want:         "user-home/.celestia-app",
		},
		{
			name:         "want celestiaHome to take precedence if both are not empty",
			userHome:     "user-home",
			celestiaHome: "celestia-home",
			want:         "celestia-home/.celestia-app",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := getDefaultNodeHome(tc.userHome, tc.celestiaHome)
			assert.Equal(t, tc.want, got)
		})
	}
}
