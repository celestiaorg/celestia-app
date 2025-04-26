package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_getDefaultNodeHome(t *testing.T) {
	type testCase struct {
		name            string
		userHome        string
		celestiaHome    string
		celestiaHomeOld string
		want            string
	}

	testCases := []testCase{
		{
			name:            "want .celestia-app when userHome is empty and celestiaHomeOld is empty",
			userHome:        "",
			celestiaHome:    "",
			celestiaHomeOld: "",
			want:            ".celestia-app",
		},
		{
			name:            "want celestia-home/.celestia-app when userHome is empty and celestiaHomeOld is not empty",
			userHome:        "",
			celestiaHome:    "",
			celestiaHomeOld: "celestia-home",
			want:            "celestia-home/.celestia-app",
		},
		{
			name:            "want user-home/.celestia-app when userHome is not empty and celestiaHomeOld is empty",
			userHome:        "user-home",
			celestiaHome:    "",
			celestiaHomeOld: "",
			want:            "user-home/.celestia-app",
		},
		{
			name:            "want celestiaHomeOld to take precedence if both are not empty",
			userHome:        "user-home",
			celestiaHome:    "",
			celestiaHomeOld: "celestia-home",
			want:            "celestia-home/.celestia-app",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := getDefaultNodeHome(tc.userHome, tc.celestiaHome, tc.celestiaHomeOld)
			assert.Equal(t, tc.want, got)
		})
	}
}
