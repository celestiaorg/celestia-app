package embedding

import _ "embed"

//go:embed celestia-app_Linux_x86_64.tar.gz
var v3binaryCompressed []byte
