package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"text/tabwriter"
	"time"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

type objectStorageCheckError struct {
	cause error
}

func (e *objectStorageCheckError) Error() string { return "object storage check failed" }
func (e *objectStorageCheckError) Unwrap() error { return e.cause }

func newStoreCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "store", Short: "Inspect the Fibre store and object storage"}
	cmd.AddCommand(newStoreStatsCmd(), newStoreMarkersCmd(), newStoreCheckBucketCmd())
	return cmd
}

func newStoreCheckBucketCmd() *cobra.Command {
	return newStoreCheckBucketCmdWithCheck(fibre.CheckObjectStorage)
}

func newStoreCheckBucketCmdWithCheck(check func(context.Context, fibre.ObjectStorageConfig) (fibre.ObjectStorageCheckResult, error)) *cobra.Command {
	var chainID, validatorAddress string
	cmd := &cobra.Command{
		Use:   "check-bucket",
		Short: "Check object storage connectivity and permissions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := cmd.Flags().GetString(flagHome)
			if err != nil {
				return fmt.Errorf("get %q flag: %w", flagHome, err)
			}
			cfg := fibre.DefaultServerConfig()
			if err := cfg.Load(fibre.DefaultConfigPath(home)); err != nil {
				return err
			}
			cfg.ObjectStorage.ChainID = chainID
			cfg.ObjectStorage.ValidatorAddress = validatorAddress
			result, err := check(cmd.Context(), cfg.ObjectStorage)
			if err != nil {
				return err
			}
			if err := writeObjectStorageCheck(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			if err := result.Err(); err != nil {
				return &objectStorageCheckError{cause: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&chainID, "chain-id", "", "chain ID for a validator-scoped canary")
	cmd.Flags().StringVar(&validatorAddress, "validator-address", "", "validator consensus address for a validator-scoped canary")
	return cmd
}

func writeObjectStorageCheck(out io.Writer, result fibre.ObjectStorageCheckResult) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "CHECK\tSTATUS")
	writeCheckResult(w, "head bucket", result.HeadBucketErr)
	writeCheckResult(w, "put canary", result.PutCanaryErr)
	writeCheckResult(w, "delete canary", result.DeleteCanaryErr)
	fmt.Fprintf(w, "\ncanary key: %s\n", result.CanaryKey)
	fmt.Fprintf(w, "credential source: %s\n", result.CredentialSource)
	fmt.Fprintf(w, "access key ID suffix: %s\n", result.AccessKeyIDSuffix)
	if !result.NamespaceScoped && hasObjectStorageErrorCode(result.PutCanaryErr, "AccessDenied") {
		fmt.Fprintln(w, "hint: path-scoped credentials may require --chain-id and --validator-address")
	}
	return w.Flush()
}

func hasObjectStorageErrorCode(err error, code string) bool {
	apiErr, ok := errors.AsType[smithy.APIError](err)
	return ok && apiErr.ErrorCode() == code
}

func writeCheckResult(w io.Writer, name string, err error) {
	status := "ok"
	if err != nil {
		status = "failed"
	}
	fmt.Fprintf(w, "%s\t%s\n", name, status)
	if err != nil {
		fmt.Fprintf(w, "  %s\n", formatObjectStorageError(err))
	}
}

func formatObjectStorageError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "request timed out"
	}
	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		return fmt.Sprintf("DNS lookup failed for %s: %s", dnsErr.Name, dnsErr.Err)
	}
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		message := fmt.Sprintf("%s: %s", apiErr.ErrorCode(), apiErr.ErrorMessage())
		if responseErr, ok := errors.AsType[*smithyhttp.ResponseError](err); ok && responseErr.HTTPStatusCode() != 0 {
			return fmt.Sprintf("HTTP %d %s", responseErr.HTTPStatusCode(), message)
		}
		return message
	}
	return err.Error()
}

func newStoreStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show read-only store statistics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := cmd.Flags().GetString(flagHome)
			if err != nil {
				return fmt.Errorf("get %q flag: %w", flagHome, err)
			}
			backend, err := loadStorageBackend(fibre.DefaultConfigPath(home))
			if err != nil {
				return err
			}
			inspector, err := fibre.OpenStoreInspector(home)
			if err != nil {
				return err
			}
			now := time.Now()
			stats, err := inspector.Stats(cmd.Context(), now)
			closeErr := inspector.Close()
			if err != nil {
				return errors.Join(err, closeErr)
			}
			if closeErr != nil {
				return closeErr
			}
			return writeStoreStats(cmd.OutOrStdout(), backend, stats, now)
		},
	}
}

func writeStoreStats(out io.Writer, backend string, stats fibre.StoreStats, now time.Time) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "storage backend\t%s\n", backend)
	fmt.Fprintf(w, "markers\t%d\n", stats.Markers)
	fmt.Fprintf(w, "local markers\t%d\n", stats.LocalMarkers)
	fmt.Fprintf(w, "local recorded bytes\t%d\n", stats.LocalBytes)
	fmt.Fprintf(w, "object markers\t%d\n", stats.ObjectMarkers)
	fmt.Fprintf(w, "object recorded bytes\t%d\n", stats.ObjectBytes)
	fmt.Fprintf(w, "legacy markers\t%d\n", stats.LegacyMarkers)
	fmt.Fprintf(w, "invalid markers\t%d\n", stats.InvalidMarkers)
	fmt.Fprintf(w, "prune entries\t%d\n", stats.PruneEntries)
	fmt.Fprintf(w, "invalid prune keys\t%d\n", stats.InvalidPruneKeys)
	fmt.Fprintf(w, "prune backlog\t%d\n", stats.PruneBacklog)
	if !stats.OldestPruneAt.IsZero() {
		fmt.Fprintf(w, "oldest prune at\t%s\n", stats.OldestPruneAt.Format(time.RFC3339))
		fmt.Fprintf(w, "oldest prune age\t%s\n", now.Sub(stats.OldestPruneAt).Round(time.Second))
	}
	if stats.ObjectNamespace != nil {
		fmt.Fprintf(w, "object endpoint\t%s\n", stats.ObjectNamespace.Endpoint)
		fmt.Fprintf(w, "object bucket\t%s\n", stats.ObjectNamespace.Bucket)
		fmt.Fprintf(w, "object prefix\t%s\n", stats.ObjectNamespace.Prefix)
		fmt.Fprintf(w, "object chain ID\t%s\n", stats.ObjectNamespace.ChainID)
		fmt.Fprintf(w, "object validator address\t%s\n", stats.ObjectNamespace.ValidatorAddress)
	}
	if stats.NamespaceError != "" {
		fmt.Fprintf(w, "object namespace error\t%s\n", stats.NamespaceError)
	}
	return w.Flush()
}

func newStoreMarkersCmd() *cobra.Command {
	var backend string
	var limit int
	cmd := &cobra.Command{
		Use:   "markers",
		Short: "List shard markers without reading payloads",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := cmd.Flags().GetString(flagHome)
			if err != nil {
				return fmt.Errorf("get %q flag: %w", flagHome, err)
			}
			inspector, err := fibre.OpenStoreInspector(home)
			if err != nil {
				return err
			}
			page, err := inspector.Markers(cmd.Context(), fibre.MarkerFilter{Backend: backend, Limit: limit})
			closeErr := inspector.Close()
			if err != nil {
				return errors.Join(err, closeErr)
			}
			if closeErr != nil {
				return closeErr
			}
			return writeStoreMarkers(cmd.OutOrStdout(), page)
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "filter by backend: local, object, or invalid")
	cmd.Flags().IntVar(&limit, "limit", 100, "maximum markers to list (0 for all)")
	return cmd
}

func writeStoreMarkers(out io.Writer, page fibre.MarkerPage) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "BACKEND\tSIZE\tCOMMITMENT\tPROMISE HASH\tKEY\tERROR")
	for _, marker := range page.Markers {
		label := marker.Backend
		if marker.Legacy {
			label += " (legacy)"
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n",
			label, marker.Size, marker.Commitment, marker.PromiseHash, marker.Key, marker.Error)
	}
	if page.Truncated {
		fmt.Fprintf(w, "showing %d markers; more exist, use --limit 0\n", len(page.Markers))
	}
	return w.Flush()
}

func loadStorageBackend(path string) (string, error) {
	backend := "local"
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return backend, nil
	}
	if err != nil {
		return "", fmt.Errorf("read config file %s: %w", path, err)
	}
	var cfg struct {
		StorageBackend string `toml:"storage_backend"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("decode config file %s: %w", path, err)
	}
	if cfg.StorageBackend != "" {
		backend = cfg.StorageBackend
	}
	if backend != "local" && backend != "object" {
		return "", fmt.Errorf("storage_backend must be local or object")
	}
	return backend, nil
}
