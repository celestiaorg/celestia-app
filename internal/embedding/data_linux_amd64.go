//go:build multiplexer

package embedding

import _ "embed"

//go:embed celestia-app_linux_v3_x86_64.tar.gz
var v3binaryCompressed []byte
