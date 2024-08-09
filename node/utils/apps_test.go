package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetApps(t *testing.T) {
	got := GetApplications()
	assert.Len(t, got, 2)
}
