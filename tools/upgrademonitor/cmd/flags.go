package cmd

// Flags
var (
	// version is the version number that should be monitored.
	version uint64
	// grpcEndpoint is the endpoint of a consensus full node.
	grpcEndpoint string
	// pollFrequency is the frequency in seconds that upgrade monitor polls the
	// GRPC endpoint.
	pollFrequency int64
	// autoTry is whether upgrademonitor will auto submit a MsgTryUpgrade if the
	// network version is upgradeable.
	autoTry bool
	// signer is the the Celestia address that should be used to submit a
	// MsgTryUpgrade if the network version is upgradeable.
	signer string
)

// Defaults
var (
	// defaultVersion is the value used if the version flag isn't provided. Since
	// v2 is coordinated via an upgrade-height, v3 is the first version that this
	// tool supports.
	// TODO (@rootulp) consider making this 3 after development.
	defaultVersion = uint64(2)
	// defaultGrpcEndpoint is the value used if the grpc-endpoint flag isn't provided.
	// This endpoint is the one enabled by default when you run ./scripts/single-node.sh
	defaultGrpcEndpoint = "0.0.0.0:9090"
	// defaultPollFrequency is the value used if the poll-frequency flag isn't provided.
	// TODO (@rootulp) consider making this 10 seconds
	defaultPollFrequency = int64(1) // 1 second
	// defaultAutoTry is the value used if the auto-try flag isn't provided.
	// TODO (@rootulp) set this to false
	defaultAutoTry = true
	// defaultSigner is the value used if the signer flag isn't provided.
	// TODO (@rootulp) set this to ""
	defaultSigner = "celestia1nh43y2t7stpa2fdfql6jutwkkn8pyr6qwmt383"
)
