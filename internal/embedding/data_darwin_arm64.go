//go:build multiplexer

package embedding

import _ "embed"

var (
	//go:embed celestia-app_darwin_v3_arm64.tar.gz
	v3binaryCompressed []byte

	//go:embed celestia-app_darwin_v4_arm64.tar.gz
	v4binaryCompressed []byte

	//go:embed celestia-app_darwin_v5_arm64.tar.gz
	v5binaryCompressed []byte
)
