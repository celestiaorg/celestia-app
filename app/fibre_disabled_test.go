//go:build !fibre

package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/stretchr/testify/assert"
)

// TestFibreModulesNotLoaded verifies that the fibre and valaddr modules are not
// loaded in the app when the fibre build tag is not set.
func TestFibreModulesNotLoaded(t *testing.T) {
	testApp := getTestApp()

	t.Run("module manager does not contain fibre or valaddr modules", func(t *testing.T) {
		assert.NotContains(t, testApp.ModuleManager.Modules, "fibre")
		assert.NotContains(t, testApp.ModuleManager.Modules, "valaddr")
	})

	t.Run("store keys do not contain fibre or valaddr", func(t *testing.T) {
		assert.Nil(t, testApp.GetKey("fibre"))
		assert.Nil(t, testApp.GetKey("valaddr"))
	})

	t.Run("encoding registers do not contain fibre or valaddr", func(t *testing.T) {
		for _, register := range app.ModuleEncodingRegisters {
			assert.NotEqual(t, "fibre", register.Name())
			assert.NotEqual(t, "valaddr", register.Name())
		}
	})
}
