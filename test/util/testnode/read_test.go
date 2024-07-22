package testnode

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReverseSlice(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected interface{}
	}{
		{[]int{1, 2, 3, 4, 5}, []int{5, 4, 3, 2, 1}},
		{[]string{"a", "b", "c", "d"}, []string{"d", "c", "b", "a"}},
		{[]int{1, 2}, []int{2, 1}},
		{[]int{1}, []int{1}},
		{[]string{}, []string{}},
	}

	for _, tt := range tests {
		switch v := tt.input.(type) {
		case []int:
			// reverseSlice modifies the input slice, so we need to make a copy
			original := make([]int, len(tt.input.([]int)))
			copy(original, tt.input.([]int))
			reverseSlice(v)
			require.True(t, reflect.DeepEqual(v, tt.expected), "reverseSlice(%v) = %v, want %v", original, tt.input, tt.expected)
		case []string:
			// reverseSlice modifies the input slice, so we need to make a copy
			original := make([]string, len(tt.input.([]string)))
			copy(original, tt.input.([]string))
			reverseSlice(v)
			require.True(t, reflect.DeepEqual(v, tt.expected), "reverseSlice(%v) = %v, want %v", original, tt.input, tt.expected)
		}
	}
}
