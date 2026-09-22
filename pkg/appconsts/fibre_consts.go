//go:build !benchmarks

package appconsts

const (
	// MaxPayForFibreMessages is the maximum number of PayForFibre messages before app v11.
	MaxPayForFibreMessages = 200
)

// GetMaxPayForFibreMessages returns the block limit for the consensus app version.
func GetMaxPayForFibreMessages(appVersion uint64) int {
	if appVersion >= 11 {
		return 2_000
	}
	return MaxPayForFibreMessages
}
