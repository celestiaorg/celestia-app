package orchestrator

// const (
// 	testAddr = "9c2B12b5a07FC6D719Ed7646e5041A7E85758329"
// 	testPriv = "64a1d6f0e760a8d62b4afdde4096f16f51b401eaaecc915740f71770ea76a8ad"
// )

// func Testclient(t *testing.T) {
// 	oc, err := newclient(
// 		context.TODO(),
// 		zerolog.New(os.Stdout),
// 		5,
// 		"tcp://localhost:26657",
// 		"tcp://localhost:9090",
// 		testPriv,
// 	)
// 	require.NoError(t, err)

// 	const subscriber = "TestBlockEvents"

// 	err = oc.tendermintRPC.Start()
// 	require.NoError(t, err)

// 	oc.watchForDataCommitments()
// 	require.NoError(t, err)

// 	err = oc.tendermintRPC.Stop()
// 	require.NoError(t, err)
// }
