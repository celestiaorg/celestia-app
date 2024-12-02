package appconsts

// Set of values that can be overridden at compile time to modify the behavior of the app.
// WARNING: This should only be modified for testing purposes. All nodes in a network
// must have the same values for these constants.
// Look at the Makefile to see how these are set.
var (
	OverrideSquareSizeUpperBoundStr string
)
