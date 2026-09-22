//go:build benchmarks

package appconsts

const (
	// MaxPayForFibreMessages arbitrary high numbers for running benchmarks.
	MaxPayForFibreMessages = 999999999999
)

// GetMaxPayForFibreMessages returns the unrestricted benchmark limit.
func GetMaxPayForFibreMessages(_ uint64) int {
	return MaxPayForFibreMessages
}
