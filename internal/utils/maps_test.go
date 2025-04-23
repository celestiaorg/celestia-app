package maps

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetField(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		path    string
		value   interface{}
		want    string
		wantErr error
	}{
		{
			name:  "set nested field",
			input: `{"a": {"b": {"c": 1}}}`,
			path:  "a.b.c",
			value: 42,
			want:  `{"a": {"b": {"c": 42}}}`,
		},
		{
			name:  "add new nested field",
			input: `{"x": {"y": {}}}`,
			path:  "x.y.z",
			value: "new",
			want:  `{"x": {"y": {"z": "new"}}}`,
		},
		{
			name:  "overwrite existing top-level field",
			input: `{"x": "old"}`,
			path:  "x",
			value: "new",
			want:  `{"x": "new"}`,
		},
		{
			name:  "add top-level field",
			input: `{"existing": 1}`,
			path:  "added",
			value: true,
			want:  `{"existing": 1, "added": true}`,
		},
		{
			name:    "invalid path - not a map",
			input:   `{"a": "not a map"}`,
			path:    "a.b",
			value:   10,
			wantErr: errors.New("invalid path"),
		},
		{
			name:    "invalid path - not a map deeper",
			input:   `{"a": {"b": 123}}`,
			path:    "a.b.c",
			value:   "fail",
			wantErr: errors.New("invalid path"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SetField([]byte(tt.input), tt.path, tt.value)

			if tt.wantErr != nil {
				require.ErrorContains(t, err, tt.wantErr.Error())
				return
			}

			require.NoError(t, err)

			gotStr := mustUnmarshal(t, string(got))
			wantStr := mustUnmarshal(t, tt.want)

			require.Equal(t, wantStr, gotStr)
		})
	}
}

func TestDeleteField(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		path    string
		want    string
		wantErr error
	}{
		{
			name:  "delete nested field",
			input: `{"a": {"b": {"c": 1}}}`,
			path:  "a.b.c",
			want:  `{"a": {"b": {}}}`,
		},
		{
			name:  "delete top-level field",
			input: `{"toDelete": 123, "keep": "yes"}`,
			path:  "toDelete",
			want:  `{"keep": "yes"}`,
		},
		{
			name:  "delete entire nested object",
			input: `{"a": {"b": {"c": {"d": 1}}}}`,
			path:  "a.b.c",
			want:  `{"a": {"b": {}}}`,
		},
		{
			name:    "delete invalid nested path",
			input:   `{"x": 5}`,
			path:    "x.y.z",
			wantErr: errors.New("invalid path"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RemoveField([]byte(tt.input), tt.path)

			if tt.wantErr != nil {
				require.ErrorContains(t, err, tt.wantErr.Error())
				return
			}

			require.NoError(t, err)

			gotStr := mustUnmarshal(t, string(got))
			wantStr := mustUnmarshal(t, tt.want)

			require.Equal(t, wantStr, gotStr)
		})
	}
}

func mustUnmarshal(t *testing.T, s string) string {
	t.Helper()
	var m map[string]interface{}
	err := json.Unmarshal([]byte(s), &m)
	require.NoError(t, err, "failed to unmarshal JSON")

	normalized, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err, "failed to re-marshal JSON")

	return string(normalized)
}
