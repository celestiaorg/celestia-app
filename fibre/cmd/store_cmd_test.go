package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

func TestWriteObjectStorageCheck(t *testing.T) {
	result := fibre.ObjectStorageCheckResult{
		CanaryKey:         "fibre/.fibre-check/canary",
		CredentialSource:  "EnvConfigCredentials",
		AccessKeyIDSuffix: "CDEF",
		HeadBucketErr:     errors.New("access denied"),
	}
	var output bytes.Buffer
	require.NoError(t, writeObjectStorageCheck(&output, result))
	require.Regexp(t, `(?m)^CHECK\s+STATUS$`, output.String())
	require.Regexp(t, `(?m)^head bucket\s+failed$`, output.String())
	require.Contains(t, output.String(), "  access denied\n")
	require.Regexp(t, `(?m)^put canary\s+ok\s*$`, output.String())
	require.Regexp(t, `(?m)^delete canary\s+ok\s*$`, output.String())
	require.Contains(t, output.String(), "canary key: "+result.CanaryKey)
	require.Contains(t, output.String(), "credential source: EnvConfigCredentials")
	require.Contains(t, output.String(), "access key ID suffix: CDEF")
}

func TestWriteObjectStorageCheckReportsIndependentDelete(t *testing.T) {
	result := fibre.ObjectStorageCheckResult{
		CanaryKey:       "fibre/.fibre-check/canary",
		PutCanaryErr:    errors.New("put denied"),
		DeleteCanaryErr: errors.New("delete denied"),
	}
	var output bytes.Buffer
	require.NoError(t, writeObjectStorageCheck(&output, result))
	require.Regexp(t, `(?m)^put canary\s+failed$`, output.String())
	require.Contains(t, output.String(), "  put denied\n")
	require.Regexp(t, `(?m)^delete canary\s+failed$`, output.String())
	require.Contains(t, output.String(), "  delete denied\n")
	require.NotContains(t, output.String(), "hint: path-scoped credentials")
}

func TestWriteObjectStorageCheckFormatsAPIErrors(t *testing.T) {
	apiErr := &smithy.GenericAPIError{Code: "AccessDenied", Message: "Access Denied", Fault: smithy.FaultClient}
	responseErr := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusForbidden}},
		Err:      apiErr,
	}
	result := fibre.ObjectStorageCheckResult{PutCanaryErr: &smithy.OperationError{
		ServiceID: "S3", OperationName: "PutObject", Err: responseErr,
	}}
	var output bytes.Buffer
	require.NoError(t, writeObjectStorageCheck(&output, result))
	require.Contains(t, output.String(), "  HTTP 403 AccessDenied: Access Denied\n")
	require.Contains(t, output.String(), "hint: path-scoped credentials may require --chain-id and --validator-address")
	require.NotContains(t, output.String(), "operation error")

	result.NamespaceScoped = true
	output.Reset()
	require.NoError(t, writeObjectStorageCheck(&output, result))
	require.NotContains(t, output.String(), "hint: path-scoped credentials")
}

func TestWriteObjectStorageCheckFormatsDNSErrors(t *testing.T) {
	result := fibre.ObjectStorageCheckResult{HeadBucketErr: &smithy.OperationError{
		ServiceID: "S3", OperationName: "HeadBucket",
		Err: &net.DNSError{Name: "missing.example.com", Err: "no such host", IsNotFound: true},
	}}
	var output bytes.Buffer
	require.NoError(t, writeObjectStorageCheck(&output, result))
	require.Contains(t, output.String(), "  DNS lookup failed for missing.example.com: no such host\n")
	require.NotContains(t, output.String(), "operation error")
}

func TestObjectStorageCheckErrorIsConciseAndUnwraps(t *testing.T) {
	cause := errors.New("put denied")
	err := &objectStorageCheckError{cause: cause}
	require.EqualError(t, err, "object storage check failed")
	require.ErrorIs(t, err, cause)
}

func TestStoreCheckBucketCmdRequiresNamespaceFlagsTogether(t *testing.T) {
	cmd := newStoreCheckBucketCmd()
	cmd.Flags().String(flagHome, t.TempDir(), "")
	cmd.SetArgs([]string{"--chain-id", "mocha-4"})
	require.EqualError(t, cmd.ExecuteContext(t.Context()), "chain ID and validator address must be provided together")
}

func TestStoreCheckBucketCmdPassesNamespaceFlags(t *testing.T) {
	home := t.TempDir()
	var checked fibre.ObjectStorageConfig
	cmd := newStoreCheckBucketCmdWithCheck(func(_ context.Context, cfg fibre.ObjectStorageConfig) (fibre.ObjectStorageCheckResult, error) {
		checked = cfg
		return fibre.ObjectStorageCheckResult{}, nil
	})
	cmd.Flags().String(flagHome, home, "")
	cmd.SetArgs([]string{"--chain-id", "mocha-4", "--validator-address", "celestiavalcons1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqnrql8a"})
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Equal(t, "mocha-4", checked.ChainID)
	require.Equal(t, "celestiavalcons1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqnrql8a", checked.ValidatorAddress)
}

func TestStoreStatsCmdDoesNotInitializeHome(t *testing.T) {
	home := t.TempDir()
	db, err := pebbledb.Open(home, &pebbledb.Options{})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	cmd := newStoreStatsCmd()
	cmd.Flags().String(flagHome, home, "")
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Regexp(t, `(?m)^storage backend\s+local$`, output.String())
	require.Regexp(t, `(?m)^markers\s+0$`, output.String())
	require.NoDirExists(t, home+"/config")
}

func TestStoreStatsCmdIgnoresUnknownConfigFields(t *testing.T) {
	home := t.TempDir()
	db, err := pebbledb.Open(home, &pebbledb.Options{})
	require.NoError(t, err)
	require.NoError(t, db.Close())
	configPath := filepath.Join(home, "config", "server_config.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("storage_backend = \"object\"\nnewer_field = true\n"), 0o600))

	cmd := newStoreStatsCmd()
	cmd.Flags().String(flagHome, home, "")
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Regexp(t, `(?m)^storage backend\s+object$`, output.String())
}

func TestStoreMarkersCmdPrintsMalformedKey(t *testing.T) {
	home := t.TempDir()
	db, err := pebbledb.Open(home, &pebbledb.Options{})
	require.NoError(t, err)
	key := "/shard/malformed"
	require.NoError(t, db.Set([]byte(key), markerValue(1, 10), pebbledb.Sync))
	require.NoError(t, db.Close())

	cmd := newStoreMarkersCmd()
	cmd.Flags().String(flagHome, home, "")
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Contains(t, output.String(), "KEY")
	require.Contains(t, output.String(), key)
	require.Contains(t, output.String(), "invalid shard key")
}

func TestStoreMarkersCmdFiltersAndLimits(t *testing.T) {
	home := t.TempDir()
	db, err := pebbledb.Open(home, &pebbledb.Options{})
	require.NoError(t, err)
	commitment := strings.Repeat("01", 32)
	require.NoError(t, db.Set([]byte("/shard/"+commitment+"/01"), markerValue(1, 10), pebbledb.Sync))
	require.NoError(t, db.Set([]byte("/shard/"+commitment+"/02"), markerValue(2, 20), pebbledb.Sync))
	require.NoError(t, db.Set([]byte("/shard/"+commitment+"/03"), markerValue(2, 30), pebbledb.Sync))
	require.NoError(t, db.Close())

	cmd := newStoreMarkersCmd()
	cmd.Flags().String(flagHome, home, "")
	cmd.SetArgs([]string{"--backend", "object", "--limit", "1"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Contains(t, output.String(), "object")
	require.Contains(t, output.String(), "/shard/"+commitment+"/02")
	require.NotContains(t, output.String(), "/shard/"+commitment+"/01")
	require.NotContains(t, output.String(), "/shard/"+commitment+"/03")
	require.Contains(t, output.String(), "more exist, use --limit 0")

	db, err = pebbledb.Open(home, &pebbledb.Options{})
	require.NoError(t, err)
	require.NoError(t, db.Set([]byte("/shard/"+commitment+"/04"), []byte{1}, pebbledb.Sync))
	require.NoError(t, db.Close())
	cmd = newStoreMarkersCmd()
	cmd.Flags().String(flagHome, home, "")
	cmd.SetArgs([]string{"--backend", "invalid"})
	output.Reset()
	cmd.SetOut(&output)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Contains(t, output.String(), "/shard/"+commitment+"/04")
	require.NotContains(t, output.String(), "/shard/"+commitment+"/01")
}

func markerValue(backend byte, size uint64) []byte {
	// Mirrors fibre.encodeShardMarkerForBackend, which is package-private.
	marker := make([]byte, 10)
	marker[0] = 1
	marker[1] = backend
	binary.BigEndian.PutUint64(marker[2:], size)
	return marker
}
