package main

import (
	"context"
	"encoding/csv"
	"net"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/cometbft/cometbft/proto/tendermint/p2p"
	tmservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type missingAccountServer struct {
	tmservice.UnimplementedServiceServer
	authtypes.UnimplementedQueryServer
	queries int
	cancel  context.CancelFunc
}

func (s *missingAccountServer) GetNodeInfo(context.Context, *tmservice.GetNodeInfoRequest) (*tmservice.GetNodeInfoResponse, error) {
	return &tmservice.GetNodeInfoResponse{DefaultNodeInfo: &p2p.DefaultNodeInfo{Network: "corto-1"}}, nil
}

func (s *missingAccountServer) Account(context.Context, *authtypes.QueryAccountRequest) (*authtypes.QueryAccountResponse, error) {
	s.queries++
	// Setup queries once; let one broadcast fail before cancelling the next.
	if s.queries == 3 {
		s.cancel()
	}
	return nil, status.Error(codes.NotFound, "account does not exist")
}

func TestMonitorRecordsBroadcastFailures(t *testing.T) {
	t.Chdir(t.TempDir())
	keyringDir := t.TempDir()
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr, err := keyring.New(app.Name, keyring.BackendTest, keyringDir, nil, encCfg.Codec)
	require.NoError(t, err)
	require.NoError(t, kr.ImportPrivKeyHex("master", "0000000000000000000000000000000000000000000000000000000000000001", "secp256k1"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := &missingAccountServer{cancel: cancel}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	tmservice.RegisterServiceServer(srv, server)
	authtypes.RegisterQueryServer(srv, server)
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	require.NoError(t, monitorLatency(ctx, lis.Addr().String(), keyringDir, "", 1, 1, "test", true, time.Millisecond, 0, 1, false, ""))
	file, err := os.Open("latency_results.csv")
	require.NoError(t, err)
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	require.NoError(t, err)
	require.Greater(t, len(rows), 1, "broadcast failures must not produce a header-only CSV")
	columns := make(map[string]int)
	for i, name := range rows[0] {
		columns[name] = i
	}
	for _, row := range rows[1:] {
		require.Equal(t, "true", row[columns["Failed"]])
		require.Contains(t, row[columns["Error"]], "account does not exist")
	}
}
