package fibre_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestClientConfigStateTLS(t *testing.T) {
	server := grpc.NewServer()
	fibretypes.RegisterQueryServer(server, &tlsParamsServer{})
	t.Cleanup(server.Stop)
	secure := httptest.NewUnstartedServer(server)
	secure.EnableHTTP2 = true
	secure.StartTLS()
	t.Cleanup(secure.Close)
	plain := httptest.NewUnstartedServer(server)
	plain.Config.Protocols = new(http.Protocols)
	plain.Config.Protocols.SetUnencryptedHTTP2(true)
	plain.Start()
	t.Cleanup(plain.Close)
	roots := x509.NewCertPool()
	roots.AddCert(secure.Certificate())

	for _, tc := range []struct {
		name    string
		address string
		config  *tls.Config
		wantErr bool
	}{
		{"trusted TLS", secure.URL, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}, false},
		{"untrusted certificate", secure.URL, &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS13}, true},
		{"wrong server name", secure.URL, &tls.Config{RootCAs: roots, ServerName: "wrong.invalid", MinVersion: tls.VersionTLS13}, true},
		{"no plaintext fallback", plain.URL, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}, true},
		{"existing plaintext default", plain.URL, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fibre.DefaultClientConfig()
			cfg.StateAddress = strings.SplitN(tc.address, "://", 2)[1]
			cfg.StateTLSConfig = tc.config
			require.NoError(t, cfg.Validate())
			client, err := cfg.StateClientFn()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Stop(context.Background())) })
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			budget, err := client.FullStakeStorageBudget(ctx)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 64, budget)
		})
	}
}

type tlsParamsServer struct {
	fibretypes.UnimplementedQueryServer
}

func (tlsParamsServer) Params(context.Context, *fibretypes.QueryParamsRequest) (*fibretypes.QueryParamsResponse, error) {
	return &fibretypes.QueryParamsResponse{Params: fibretypes.Params{FullStakeStorageBudget: 64}}, nil
}
