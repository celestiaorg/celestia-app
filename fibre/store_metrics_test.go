package fibre

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cockroachdb/pebble/v2/vfs"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestBackendGetMetrics(t *testing.T) {
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: []byte("data")}}}
	var encoded bytes.Buffer
	require.NoError(t, writeShardBinary(&encoded, shard))
	for _, backend := range []string{storageBackendLocal, storageBackendObject} {
		for _, outcome := range []string{"success", "not_found", "error"} {
			t.Run(backend+"/"+outcome, func(t *testing.T) {
				metrics, reader := newBackendTestMetrics(t)
				data := encoded.Bytes()
				if outcome == "error" {
					data = data[:len(data)-1]
				}
				var b shardBackend
				if backend == storageBackendLocal {
					local, err := newLocalBackend("/store", vfs.NewMem())
					require.NoError(t, err)
					local.metrics = metrics
					if outcome != "not_found" {
						f, err := local.fs.Create(local.shardPath(Commitment{}, nil), shardPayloadWriteCategory)
						require.NoError(t, err)
						_, err = f.Write(bytes.Clone(data))
						require.NoError(t, err)
						require.NoError(t, f.Close())
					}
					b = local
				} else {
					object := newObjectBackend(&s3ObjectClientStub{
						getObject: func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
							if outcome == "not_found" {
								return nil, &smithy.GenericAPIError{Code: "NoSuchKey"}
							}
							return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(data))}, nil
						},
					}, "bucket", "prefix", "chain", "validator")
					object.metrics = metrics
					b = object
				}
				got, err := b.Get(t.Context(), Commitment{}, nil)
				if outcome == "success" {
					require.NoError(t, err)
					require.Equal(t, shard, got)
				} else {
					require.Error(t, err)
				}
				wantBytes := int64(len(data))
				if outcome == "not_found" {
					wantBytes = 0
				}
				assertBackendGetMetrics(t, reader, backend, outcome, wantBytes)
			})
		}
	}
}

func TestObjectBackendGetErrorMetrics(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		err           error
	}{
		{"deadline", "timeout", context.DeadlineExceeded},
		{"canceled", "canceled", context.Canceled},
		{"slow down", "throttled", &smithy.GenericAPIError{Code: "SlowDown"}},
		{"429", "throttled", &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusTooManyRequests}},
			Err:      errors.New("too many requests"),
		}},
		{"other", "error", errors.New("arbitrary error text")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metrics, reader := newBackendTestMetrics(t)
			object := newObjectBackend(&s3ObjectClientStub{
				getObject: func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
					return nil, fmt.Errorf("request failed: %w", tc.err)
				},
			}, "bucket", "prefix", "chain", "validator")
			object.metrics = metrics
			_, err := object.Get(t.Context(), Commitment{}, nil)
			require.ErrorIs(t, err, tc.err)
			assertBackendGetMetrics(t, reader, storageBackendObject, tc.outcome, 0)
		})
	}
}

func TestObjectBackendGetMetricsDuringRead(t *testing.T) {
	metrics, reader := newBackendTestMetrics(t)
	reads := 0
	body := &metricTestBody{
		read: func(p []byte) (int, error) {
			reads++
			if reads == 1 {
				return copy(p, []byte{0, 0, 0, 1}), nil
			}
			data := collectBackendMetrics(t, reader)
			require.Equal(t, int64(1), data["in_flight"].Data.(metricdata.Sum[int64]).DataPoints[0].Value)
			require.Equal(t, int64(4), data["bytes"].Data.(metricdata.Sum[int64]).DataPoints[0].Value)
			_, recorded := data["duration"]
			require.False(t, recorded)
			return copy(p, []byte{0, 0}), context.DeadlineExceeded
		},
		close: func() error {
			data := collectBackendMetrics(t, reader)
			require.Equal(t, int64(1), data["in_flight"].Data.(metricdata.Sum[int64]).DataPoints[0].Value)
			_, recorded := data["duration"]
			require.False(t, recorded, "duration must include closing the body")
			return nil
		},
	}
	object := newObjectBackend(&s3ObjectClientStub{
		getObject: func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			return &s3.GetObjectOutput{Body: body}, nil
		},
	}, "bucket", "prefix", "chain", "validator")
	object.metrics = metrics
	_, err := object.Get(t.Context(), Commitment{}, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assertBackendGetMetrics(t, reader, storageBackendObject, "timeout", 6)
}

type metricTestBody struct {
	read  func([]byte) (int, error)
	close func() error
}

func (b *metricTestBody) Read(p []byte) (int, error) { return b.read(p) }
func (b *metricTestBody) Close() error               { return b.close() }

func newBackendTestMetrics(t *testing.T) (*serverMetrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	metrics, err := newServerMetrics(provider.Meter("backend-test"), newOccupancy(0))
	require.NoError(t, err)
	return metrics, reader
}

func collectBackendMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	result := make(map[string]metricdata.Metrics)
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			for _, name := range []string{"duration", "in_flight", "bytes"} {
				if m.Name == "fibre.server.backend.get."+name {
					result[name] = m
				}
			}
		}
	}
	return result
}

func assertBackendGetMetrics(t *testing.T, reader *sdkmetric.ManualReader, backend, outcome string, wantBytes int64) {
	t.Helper()
	data := collectBackendMetrics(t, reader)
	duration := data["duration"].Data.(metricdata.Histogram[float64])
	require.Len(t, duration.DataPoints, 1)
	require.Equal(t, uint64(1), duration.DataPoints[0].Count)
	require.Positive(t, duration.DataPoints[0].Sum)
	require.Equal(t, attribute.NewSet(attribute.String("backend", backend), attribute.String("outcome", outcome)), duration.DataPoints[0].Attributes)
	inFlight := data["in_flight"].Data.(metricdata.Sum[int64])
	require.Len(t, inFlight.DataPoints, 1)
	require.Equal(t, int64(0), inFlight.DataPoints[0].Value)
	require.Equal(t, attribute.NewSet(attribute.String("backend", backend)), inFlight.DataPoints[0].Attributes)
	if wantBytes == 0 {
		_, recorded := data["bytes"]
		require.False(t, recorded)
	} else {
		readBytes := data["bytes"].Data.(metricdata.Sum[int64])
		require.Len(t, readBytes.DataPoints, 1)
		require.Equal(t, wantBytes, readBytes.DataPoints[0].Value)
		require.Equal(t, attribute.NewSet(attribute.String("backend", backend)), readBytes.DataPoints[0].Attributes)
	}
}
