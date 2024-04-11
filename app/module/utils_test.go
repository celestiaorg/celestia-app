package module

import (
	"strings"
	"testing"

	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
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

func Test_getKeys(t *testing.T) {
	type testCase struct {
		input map[uint64]map[string]sdkmodule.AppModule
		want  []uint64
	}
	testCases := []testCase{
		{
			input: map[uint64]map[string]sdkmodule.AppModule{},
			want:  []uint64{},
		},
		{
			input: map[uint64]map[string]sdkmodule.AppModule{
				1: {"a": nil},
			},
			want: []uint64{1},
		},
		{
			input: map[uint64]map[string]sdkmodule.AppModule{
				1: {"a": nil},
				3: {"b": nil},
			},
			want: []uint64{1, 3},
		},
	}
	for _, tc := range testCases {
		got := getKeys(tc.input)
		assert.Equal(t, tc.want, got)
	}
}
