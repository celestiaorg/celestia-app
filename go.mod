module github.com/celestiaorg/celestia-app

go 1.15

require (
	github.com/celestiaorg/celestia-core v0.0.1-mvp-das-lightclient.0.20210831143948-ceaf5e5c3eec
	github.com/celestiaorg/nmt v0.6.0
	github.com/cosmos/cosmos-sdk v0.40.0-rc5
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.2
	github.com/golang/snappy v0.0.3-0.20201103224600-674baa8c7fc3 // indirect
	github.com/gorilla/mux v1.8.0
	github.com/grpc-ecosystem/grpc-gateway v1.16.0
	github.com/pelletier/go-toml v1.9.3
	github.com/regen-network/cosmos-proto v0.3.1
	github.com/rs/zerolog v1.23.0
	github.com/spf13/cast v1.3.1
	github.com/spf13/cobra v1.2.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	github.com/tendermint/tm-db v0.6.3
	golang.org/x/term v0.0.0-20210220032956-6a3ed077a48d // indirect
	google.golang.org/genproto v0.0.0-20210830153122-0bac4d21c8ea
	google.golang.org/grpc v1.40.0

)

replace (
	github.com/cosmos/cosmos-sdk v0.40.0-rc5 => github.com/celestiaorg/cosmos-sdk v0.40.0-rc5.0.20210831150455-4a354186ed7a
	github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.2-alpha.regen.4
)
