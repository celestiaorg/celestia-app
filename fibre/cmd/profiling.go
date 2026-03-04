package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	httppprof "net/http/pprof"
	"os"
	"runtime"

	"github.com/grafana/pyroscope-go"
	"github.com/spf13/cobra"
)

const (
	flagPProf           = "pprof"
	defaultPProfAddress = "localhost:6060"

	flagPyroscopeEndpoint      = "pyroscope-endpoint"
	envPyroscopeEndpoint       = "FIBRE_PYROSCOPE_ENDPOINT"
	flagPyroscopeBasicAuthUser = "pyroscope-basic-auth-user"
	envPyroscopeBasicAuthUser  = "FIBRE_PYROSCOPE_BASIC_AUTH_USER"
	flagPyroscopeBasicAuthPass = "pyroscope-basic-auth-password"
	envPyroscopeBasicAuthPass  = "FIBRE_PYROSCOPE_BASIC_AUTH_PASSWORD"
)

// registerProfilingFlags adds pprof and Pyroscope persistent flags to cmd and
// applies corresponding environment variable overrides if set.
func registerProfilingFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(flagPProf, "", fmt.Sprintf("enable pprof HTTP server (use --pprof for default address %s, or --pprof=<addr> for custom)", defaultPProfAddress))
	// NoOptDefVal makes bare --pprof equivalent to --pprof=localhost:6060.
	cmd.PersistentFlags().Lookup(flagPProf).NoOptDefVal = defaultPProfAddress

	cmd.PersistentFlags().String(flagPyroscopeEndpoint, "", fmt.Sprintf("Pyroscope endpoint for continuous profiling, e.g. http://localhost:4040 (or set %s)", envPyroscopeEndpoint))
	cmd.PersistentFlags().String(flagPyroscopeBasicAuthUser, "", fmt.Sprintf("Pyroscope basic auth username (or set %s)", envPyroscopeBasicAuthUser))
	cmd.PersistentFlags().String(flagPyroscopeBasicAuthPass, "", fmt.Sprintf("Pyroscope basic auth password (or set %s)", envPyroscopeBasicAuthPass))

	setPersistentFlagFromEnv(cmd, flagPyroscopeEndpoint, envPyroscopeEndpoint)
	setPersistentFlagFromEnv(cmd, flagPyroscopeBasicAuthUser, envPyroscopeBasicAuthUser)
	setPersistentFlagFromEnv(cmd, flagPyroscopeBasicAuthPass, envPyroscopeBasicAuthPass)
}

// setupPProfServer reads the pprof-address flag and starts a /debug/pprof HTTP
// server in the background. If the address is empty, it is a no-op. The
// returned stop function shuts the server down gracefully.
func setupPProfServer(cmd *cobra.Command) (func(), error) {
	addr, err := cmd.Flags().GetString(flagPProf)
	if err != nil {
		return nil, fmt.Errorf("get %q flag: %w", flagPProf, err)
	}
	if addr == "" {
		return func() {}, nil
	}

	// Enable mutex and block profiling; both are off by default in the runtime.
	// fraction=5 samples ~20% of mutex contention events (lower = more overhead).
	// rate=1 records every blocking event (raise if overhead is a concern).
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(1)

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", httppprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", httppprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", httppprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", httppprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", httppprof.Trace)

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		slog.Info("pprof server started", "address", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("pprof server error", "error", err)
		}
	}()

	return func() {
		if err := srv.Shutdown(context.Background()); err != nil {
			slog.Error("shutting down pprof server", "error", err)
		}
	}, nil
}

// setupProfiling reads Pyroscope flags from cmd and starts the continuous
// profiler. Trace-profile correlation (pprof goroutine labels) is handled
// automatically by setupTracing when tracing is enabled. If the endpoint is
// empty, profiling is skipped and a no-op stop function is returned. The
// returned stop function must be called on exit to flush remaining profiles.
func setupProfiling(cmd *cobra.Command) (func(), error) {
	endpoint, err := cmd.Flags().GetString(flagPyroscopeEndpoint)
	if err != nil {
		return nil, fmt.Errorf("get %q flag: %w", flagPyroscopeEndpoint, err)
	}
	if endpoint == "" {
		return func() {}, nil
	}

	basicAuthUser, err := cmd.Flags().GetString(flagPyroscopeBasicAuthUser)
	if err != nil {
		return nil, fmt.Errorf("get %q flag: %w", flagPyroscopeBasicAuthUser, err)
	}
	basicAuthPass, err := cmd.Flags().GetString(flagPyroscopeBasicAuthPass)
	if err != nil {
		return nil, fmt.Errorf("get %q flag: %w", flagPyroscopeBasicAuthPass, err)
	}

	hostname, _ := os.Hostname()
	profiler, err := pyroscope.Start(pyroscope.Config{
		ApplicationName:   "fibre",
		ServerAddress:     endpoint,
		BasicAuthUser:     basicAuthUser,
		BasicAuthPassword: basicAuthPass,
		Logger:            &pyroscopeSlogAdapter{},
		Tags:              map[string]string{"version": version, "hostname": hostname},
	})
	if err != nil {
		return nil, fmt.Errorf("starting Pyroscope profiler: %w", err)
	}
	slog.Info("profiling enabled", "endpoint", endpoint)

	return func() {
		if err := profiler.Stop(); err != nil {
			slog.Error("stopping Pyroscope profiler", "error", err)
		}
	}, nil
}

// pyroscopeSlogAdapter bridges the pyroscope.Logger interface to slog.
type pyroscopeSlogAdapter struct{}

func (a *pyroscopeSlogAdapter) Infof(format string, args ...interface{}) {
	slog.Info(fmt.Sprintf(format, args...))
}

func (a *pyroscopeSlogAdapter) Debugf(format string, args ...interface{}) {
	slog.Debug(fmt.Sprintf(format, args...))
}

func (a *pyroscopeSlogAdapter) Errorf(format string, args ...interface{}) {
	slog.Error(fmt.Sprintf(format, args...))
}
