//go:build multiplexer

package embedding

import _ "embed"

var (
	//go:embed celestia-app_linux_v3_amd64.tar.gz
	v3binaryCompressed []byte

	//go:embed celestia-app_linux_v4_amd64.tar.gz
	v4binaryCompressed []byte

	//go:embed celestia-app_linux_v5_amd64.tar.gz
	v5binaryCompressed []byte
)
