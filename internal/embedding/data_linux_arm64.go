package embedding

import _ "embed"

//go:embed celestia-app_Linux_arm64.tar.gz
var v3binaryCompressed []byte
