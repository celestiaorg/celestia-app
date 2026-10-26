// Package hardfork embeds the validator recovery script so the binary can
// print it.
package hardfork

import _ "embed"

// RecoveryScript is recovery.sh from this directory.
//
//go:embed recovery.sh
var RecoveryScript string
