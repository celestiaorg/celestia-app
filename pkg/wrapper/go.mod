module github.com/celestiaorg/celestia-app/pkg/wrapper

go 1.21.1

require (
	github.com/celestiaorg/celestia-app/pkg/appconsts v0.0.0
	github.com/celestiaorg/celestia-app/pkg/namespace v0.0.0
	github.com/celestiaorg/nmt v0.18.1
	github.com/celestiaorg/rsmt2d v0.11.0
)

require (
	github.com/celestiaorg/merkletree v0.0.0-20210714075610-a84dc3ddbbe4 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/klauspost/cpuid/v2 v2.1.1 // indirect
	github.com/klauspost/reedsolomon v1.11.8 // indirect
	golang.org/x/sync v0.3.0 // indirect
	golang.org/x/sys v0.1.0 // indirect
)

replace (
	github.com/celestiaorg/celestia-app/pkg/appconsts => ../appconsts
	github.com/celestiaorg/celestia-app/pkg/namespace => ../namespace
)
