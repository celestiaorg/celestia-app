package fibre

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/cometbft/cometbft/crypto"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDependencyErrorsStayInServerLogs(t *testing.T) {
	for _, stage := range []string{"verify promise", "validator set", "public key", "sign"} {
		t.Run(stage, func(t *testing.T) {
			server, request := newCoordinatedUploadTest(t)
			const addr = "10.20.30.40:9090"
			depErr := status.Error(codes.Unavailable, `connection error: desc = "transport: Error while dialing: dial tcp `+addr+`: connect: connection refused"`)
			st := &failingDependencyState{uploadTestState: server.state.(*uploadTestState)}
			server.state = st
			var logs strings.Builder
			server.log = slog.New(slog.NewTextHandler(&logs, nil))

			var want string
			wantCode := codes.InvalidArgument
			switch stage {
			case "verify promise":
				st.verifyErr = depErr
				want = "payment promise verification failed: app unavailable"
			case "validator set":
				st.valSetErr = depErr
				want = "shard assignment verification failed: dependency unavailable"
			case "public key":
				server.signer = failingSigner{PrivValidator: server.signer, pubKeyErr: depErr}
				want = "shard assignment verification failed: dependency unavailable"
			case "sign":
				server.signer = failingSigner{PrivValidator: server.signer, signErr: depErr}
				want, wantCode = "failed to sign payment promise", codes.Internal
			}
			_, err := server.UploadShard(t.Context(), request(0))
			require.Equal(t, wantCode, status.Code(err))
			require.Equal(t, want, status.Convert(err).Message())
			require.Contains(t, logs.String(), addr)
			if stage != "sign" {
				// Signing runs after the shard is stored; a retry re-signs it.
				require.Zero(t, server.occ.usage())
			}
		})
	}
}

// A rejected promise still reaches the client with its reason.
func TestRejectedPromiseKeepsReason(t *testing.T) {
	server, request := newCoordinatedUploadTest(t)
	st := &failingDependencyState{uploadTestState: server.state.(*uploadTestState)}
	st.verifyErr = status.Error(codes.Internal, "escrow account not found")
	server.state = st
	_, err := server.UploadShard(t.Context(), request(0))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "escrow account not found")
}

type failingDependencyState struct {
	*uploadTestState
	verifyErr, valSetErr error
}

func (s *failingDependencyState) VerifyPromise(ctx context.Context, p *state.PaymentPromise) (state.VerifiedPromise, error) {
	if s.verifyErr != nil {
		return state.VerifiedPromise{}, s.verifyErr
	}
	return s.uploadTestState.VerifyPromise(ctx, p)
}

func (s *failingDependencyState) GetByHeight(ctx context.Context, height uint64) (validator.Set, error) {
	if s.valSetErr != nil {
		return validator.Set{}, s.valSetErr
	}
	return s.uploadTestState.GetByHeight(ctx, height)
}

type failingSigner struct {
	core.PrivValidator
	signErr, pubKeyErr error
}

func (s failingSigner) SignRawBytes(string, string, []byte) ([]byte, error) { return nil, s.signErr }

func (s failingSigner) GetPubKey() (crypto.PubKey, error) {
	if s.pubKeyErr != nil {
		return nil, s.pubKeyErr
	}
	return s.PrivValidator.GetPubKey()
}
