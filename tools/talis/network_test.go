package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFibreParamsModifierValidates(t *testing.T) {
	_, err := fibreParamsModifier(0, 10*time.Minute, 10*time.Minute)
	require.NoError(t, err)
	_, err = fibreParamsModifier(0, 5*time.Minute, 0)
	require.ErrorContains(t, err, "shard retention must be at least 10m0s")
	_, err = fibreParamsModifier(0, 0, 5*time.Minute)
	require.ErrorContains(t, err, "payment promise timeout must be at least 10m0s")
}
