//go:build multiplexer

package embedding

import _ "embed"

//go:embed celestia-app_darwin_v3_arm64.tar.gz
var v3binaryCompressed []byte
