//go:build fibre

package cmd

// isFibreEnabled reports whether the binary was built with the fibre build tag.
func isFibreEnabled() bool {
	return true
}
