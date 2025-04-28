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
			name:            "want .celestia-app when userHome is empty and celestiaHome/Old are empty",
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
			name:            "want user-home/.celestia-app when userHome is not empty and celestiaHome/Old are empty",
			userHome:        "user-home",
			celestiaHome:    "",
			celestiaHomeOld: "",
			want:            "user-home/.celestia-app",
		},
		{
			name:            "want celestiaHomeOld to take precedence if both it and userHome are not empty",
			userHome:        "user-home",
			celestiaHome:    "",
			celestiaHomeOld: "celestia-home",
			want:            "celestia-home/.celestia-app",
		},
		{
			name:            "want celestia-home when userHome is empty and celestiaHome is not empty",
			userHome:        "",
			celestiaHome:    "celestia-home",
			celestiaHomeOld: "",
			want:            "celestia-home",
		},
		{
			name:            "want celestiaHome to take precedence if both it and userHome are not empty",
			userHome:        "user-home",
			celestiaHome:    "celestia-home",
			celestiaHomeOld: "",
			want:            "celestia-home",
		},
		{
			name:            "want celestiaHome to take precedence if both it and celestiaHomeOld are not empty",
			userHome:        "",
			celestiaHome:    "celestia-home",
			celestiaHomeOld: "celestia-home/.celestia-appd",
			want:            "celestia-home",
		},
		{
			name:            "want celestiaHome to take precedence if all are not empty",
			userHome:        "user-home",
			celestiaHome:    "celestia-home",
			celestiaHomeOld: "celestia-home/.celestia-appd",
			want:            "celestia-home",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := getDefaultNodeHome(tc.userHome, tc.celestiaHome, tc.celestiaHomeOld)
			assert.Equal(t, tc.want, got)
		})
	}
}
