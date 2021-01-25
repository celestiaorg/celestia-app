module github.com/lazyledger/lazyledger-app

go 1.15

require (
	github.com/cosmos/cosmos-sdk v0.41.0
	github.com/cosmos/gaia/v4 v4.0.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.4.3
	github.com/gorilla/mux v1.8.0
	github.com/grpc-ecosystem/grpc-gateway v1.16.0
	github.com/pelletier/go-toml v1.8.0
	github.com/regen-network/cosmos-proto v0.3.0
	github.com/spf13/cast v1.3.1
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	github.com/tendermint/tendermint v0.34.3
	github.com/tendermint/tm-db v0.6.3
	golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad // indirect
	golang.org/x/sys v0.0.0-20210124154548-22da62e12c0c // indirect
	golang.org/x/term v0.0.0-20201210144234-2321bbc49cbf // indirect
	google.golang.org/genproto v0.0.0-20201119123407-9b1e624d6bc4
	google.golang.org/grpc v1.35.0

)

replace (
	github.com/cosmos/cosmos-sdk => github.com/lazyledger/cosmos-sdk v0.40.0-rc5
	github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.2-alpha.regen.4
)
