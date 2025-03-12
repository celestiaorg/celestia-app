package embedding

import _ "embed"

// TODO: allow for multiple embedded versions.

//go:embed celestia-app_Darwin_arm64.tar.gz
var v3binaryCompressed []byte
