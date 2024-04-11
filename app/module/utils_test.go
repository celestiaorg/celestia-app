package module

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_defaultMigrationsOrder(t *testing.T) {
	type testCase struct {
		input []string
		want  []string
	}
	testCases := []testCase{
		{
			input: []string{"auth"},
			want:  []string{"auth"},
		},
		{
			input: []string{"auth", "bank", "staking"},
			want:  []string{"bank", "staking", "auth"},
		},
		{
			input: []string{"staking", "bank"},
			want:  []string{"bank", "staking"},
		},
	}
	for _, tc := range testCases {
		t.Run(strings.Join(tc.input, ", "), func(t *testing.T) {
			got := defaultMigrationsOrder(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}

}
