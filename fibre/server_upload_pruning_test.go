package fibre

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShardPruneAt(t *testing.T) {
	creation := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	const retention = 4 * time.Hour

	tests := []struct {
		name      string
		expiresAt time.Time
		want      time.Time
	}{
		{"retention floor beats short expiry", creation.Add(time.Hour), creation.Add(retention)},
		{"long expiry beats retention floor", creation.Add(8 * time.Hour), creation.Add(8 * time.Hour)},
		{"expiry equals floor", creation.Add(retention), creation.Add(retention)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shardPruneAt(creation, tt.expiresAt, retention)
			require.True(t, got.Equal(tt.want), "got %v, want %v", got, tt.want)
		})
	}
}
