module github.com/lazyledger/lazyledger-app

go 1.15

require (
	github.com/cosmos/cosmos-sdk v0.40.0-rc5
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.4.3
	github.com/golang/snappy v0.0.3-0.20201103224600-674baa8c7fc3 // indirect
	github.com/gorilla/mux v1.8.0
	github.com/grpc-ecosystem/grpc-gateway v1.16.0
	github.com/lazyledger/lazyledger-core v0.0.0-20210122184344-b83e6766973c
	github.com/pelletier/go-toml v1.8.0
	github.com/regen-network/cosmos-proto v0.3.0
	github.com/rs/zerolog v1.20.0
	github.com/spf13/cast v1.3.1
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/tendermint/tm-db v0.6.3
	golang.org/x/net v0.0.0-20201021035429-f5854403a974 // indirect
	golang.org/x/sys v0.0.0-20210124154548-22da62e12c0c // indirect
	golang.org/x/term v0.0.0-20201210144234-2321bbc49cbf // indirect
	google.golang.org/genproto v0.0.0-20210114201628-6edceaf6022f
	google.golang.org/grpc v1.33.2

)

replace (
	github.com/cosmos/cosmos-sdk => github.com/lazyledger/cosmos-sdk v0.40.0-rc5.0.20210121152417-3addd7f65d1c
	github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.2-alpha.regen.4
)
