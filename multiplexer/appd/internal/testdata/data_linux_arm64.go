package testdata

import _ "embed"

//go:embed celestia-app_Linux_arm64.tar.gz
var binaryCompressed []byte
