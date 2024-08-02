package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetApps(t *testing.T) {
	got := GetApps()
	assert.Len(t, got, 1)
}
