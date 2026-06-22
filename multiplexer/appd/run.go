package appd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Appd represents a celestia-appd binary.
type Appd struct {
	// version is the version of the celestia-appd binary.
	// Example: "v3.10.0-arabica"
	version string
	// path is the path to the celestia-appd binary.
	path   string
	stdin  io.Reader
	stderr io.Writer
	stdout io.Writer

	// mu guards the fields below which are accessed by both the caller and the
	// goroutine that waits on the running process.
	mu sync.Mutex
	// cmd is the started celestia-appd binary.
	cmd *exec.Cmd
	// waitCh is closed when the process started by the most recent call to
	// Start exits. It is nil until Start is called.
	waitCh chan struct{}
	// waitErr is the error returned by cmd.Wait. It is only valid to read after
	// waitCh has been closed.
	waitErr error
	// stopping is set to true when Stop is called so that the process exit is
	// recognised as intentional rather than a crash.
	stopping bool
}

// New returns a new Appd instance.
func New(version string, compressedBinary []byte) (*Appd, error) {
	if len(compressedBinary) == 0 {
		return nil, fmt.Errorf("no compressed binary available for version %s", version)
	}

	if err := ensureBinaryDecompressed(version, compressedBinary); err != nil {
		return nil, fmt.Errorf("failed to decompress binary: %w", err)
	}

	pathToBinary, err := getPathToBinary(version)
	if err != nil {
		return nil, fmt.Errorf("failed to get path to binary: %w", err)
	}

	appd := &Appd{
		version: version,
		path:    pathToBinary,
		stdin:   os.Stdin,
		stdout:  os.Stdout,
		stderr:  os.Stderr,
	}
	return appd, nil
}

// telemetryDisableEnv returns environment variables that disable the
// Prometheus telemetry sink in the child process. This prevents
// "duplicate metrics collector registration attempted" errors.
// The env var prefix must match the Viper env prefix used by the child
// process (envPrefix = "CELESTIA_APP"), not the binary filename.
func (a *Appd) telemetryDisableEnv() []string {
	return []string{
		envPrefix + "_TELEMETRY_PROMETHEUS_RETENTION_TIME=0",
	}
}

// getEnv returns the environment variables for the child process. It
// starts with the current process's full environment (os.Environ()) and
// appends the telemetry disable variables. We must explicitly include
// os.Environ() because exec.Cmd.Env defaults to nil (inherit parent env),
// but once set to a non-nil slice it uses ONLY that slice.
func (a *Appd) getEnv() []string {
	return append(os.Environ(), a.telemetryDisableEnv()...)
}

// Start starts the appd binary with the given arguments.
func (a *Appd) Start(args ...string) error {
	cmd := exec.Command(a.path, append([]string{"start"}, args...)...)
	cmd.Env = a.getEnv()

	// Set up I/O
	cmd.Stdin = a.stdin
	cmd.Stdout = a.stdout
	cmd.Stderr = a.stderr

	// Start the embedded binary in its own process group.
	// This prevents the embedded binary from receiving CTRL+C signals directly from the terminal.
	// That way, the multiplexer can shut down the embedded binary after shutting down CometBFT.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", a.path, err)
	}

	// Wait on the process from a single goroutine. Centralizing the cmd.Wait
	// call here means both Stop and Wait can observe the exit via waitCh without
	// racing on a second cmd.Wait call.
	waitCh := make(chan struct{})
	a.mu.Lock()
	a.cmd = cmd
	a.waitCh = waitCh
	a.waitErr = nil
	a.stopping = false
	a.mu.Unlock()

	go func() {
		err := cmd.Wait()
		a.mu.Lock()
		a.waitErr = err
		a.mu.Unlock()
		close(waitCh)
	}()

	return nil
}

func (a *Appd) IsRunning() bool {
	return !a.IsStopped()
}

func (a *Appd) IsStopped() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Never started or failed to start.
	if a.cmd == nil || a.cmd.Process == nil || a.waitCh == nil {
		return true
	}

	// If waitCh is closed, the process has exited.
	select {
	case <-a.waitCh:
		return true
	default:
		return false
	}
}

// Wait blocks until the running process exits and reports whether the exit was
// unexpected. It returns:
//   - nil if the process was never started or was stopped intentionally via
//     Stop.
//   - a non-nil error if the process exited on its own, regardless of the exit
//     code, since the multiplexer never expects an embedded binary to exit
//     while it is still running.
func (a *Appd) Wait() error {
	a.mu.Lock()
	waitCh := a.waitCh
	a.mu.Unlock()

	if waitCh == nil {
		return nil
	}

	<-waitCh

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopping {
		return nil
	}
	if a.waitErr != nil {
		return fmt.Errorf("embedded app exited unexpectedly: %w", a.waitErr)
	}
	return fmt.Errorf("embedded app exited unexpectedly")
}

// Stop interrupts and then kills the running appd process if it exists and
// waits for it to fully exit. If the process is not running, it returns nil.
// The method will wait up to 6 seconds for graceful shutdown before force killing.
func (a *Appd) Stop() error {
	a.mu.Lock()
	if a.cmd == nil || a.cmd.Process == nil || a.waitCh == nil {
		a.mu.Unlock()
		return nil
	}
	// Mark the shutdown as intentional before signalling the process so that the
	// Wait observer does not interpret the exit as a crash.
	a.stopping = true
	process := a.cmd.Process
	waitCh := a.waitCh
	a.mu.Unlock()

	if err := process.Signal(os.Interrupt); err != nil {
		log.Printf("Failed to send interrupt signal, attempting to kill: %v", err)
		if err := process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process with PID %d: %w", process.Pid, err)
		}

		<-waitCh
		if err := a.exitErr(); err != nil {
			log.Printf("Process finished with error: %v\n", err)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	select {
	case <-waitCh:
		if err := a.exitErr(); err != nil {
			log.Printf("Process finished with error: %v\n", err)
		} else {
			log.Printf("Process finished with no error\n")
		}
		return nil
	case <-ctx.Done():
		log.Printf("Process did not exit within 6 seconds, force killing")
		if err := process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process with PID %d after timeout: %w", process.Pid, err)
		}

		<-waitCh
		if err := a.exitErr(); err != nil {
			log.Printf("Process finished with error after force kill: %v\n", err)
		} else {
			log.Printf("Process finished after force kill\n")
		}
		return nil
	}
}

// exitErr returns the error returned by the underlying process. It must only be
// called after the process has exited (waitCh closed).
func (a *Appd) exitErr() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.waitErr
}

// CreateExecCommand creates an exec.Cmd for the appd binary.
func (a *Appd) CreateExecCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(a.path, args...)
	cmd.Stdin = a.stdin
	cmd.Stdout = a.stdout
	cmd.Stderr = a.stderr
	return cmd
}

// getPathToBinary returns the path to the celestia-appd binary for the given version.
func getPathToBinary(version string) (string, error) {
	var pathToBinary string
	baseDirectory := getDirectoryForVersion(version)

	// look for the executable binary in the extracted files
	err := filepath.Walk(baseDirectory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && info.Mode()&0o111 != 0 {
			pathToBinary = path
			return filepath.SkipAll // Found it, stop searching
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to find executable binary in the archive: %w", err)
	}
	if pathToBinary == "" {
		return "", fmt.Errorf("no executable binary found in the archive for %s", version)
	}
	return pathToBinary, nil
}

// ensureBinaryDecompressed decompresses the binary for the given version if it
// is not already decompressed.
func ensureBinaryDecompressed(version string, binary []byte) error {
	if isBinaryDecompressed(version) {
		return nil
	}

	// untar the binary.
	gzipReader, err := gzip.NewReader(bytes.NewReader(binary))
	if err != nil {
		return fmt.Errorf("failed to read binary data for %s: %w", version, err)
	}
	defer gzipReader.Close()

	targetDirectory := getDirectoryForVersion(version)
	if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// extract all files from the tar archive to the directory
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		if header.FileInfo().IsDir() {
			// Create directory
			dirPath, err := sanitizeTarPath(targetDirectory, header.Name)
			if err != nil {
				return fmt.Errorf("path traversal in tar entry: %w", err)
			}
			if err := os.MkdirAll(dirPath, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dirPath, err)
			}
			continue
		}

		// Create file path
		filePath, err := sanitizeTarPath(targetDirectory, header.Name)
		if err != nil {
			return fmt.Errorf("path traversal in tar entry: %w", err)
		}

		// Create parent directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("failed to create parent directory for %s: %w", filePath, err)
		}

		// Create file
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, header.FileInfo().Mode())
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", filePath, err)
		}

		if _, err := io.Copy(f, tarReader); err != nil {
			f.Close()
			return fmt.Errorf("failed to copy file contents to %s: %w", filePath, err)
		}
		f.Close()
	}

	return nil
}

// isBinaryDecompressed returns true if the binary for the given version
// has already been decompressed.
func isBinaryDecompressed(version string) bool {
	dir := getDirectoryForVersion(version)
	_, err := os.Stat(dir)
	return err == nil
}

// getDirectoryForCelestiaAppBinaries returns the directory where all
// decompressed celestia-app binaries are stored. One directory exists per
// version.
func getDirectoryForCelestiaAppBinaries() string {
	return filepath.Join(nodeHome, "bin")
}

// getDirectoryForVersion returns the directory for a particular version.
func getDirectoryForVersion(version string) string {
	return filepath.Join(getDirectoryForCelestiaAppBinaries(), version)
}

// sanitizeTarPath validates that the tar entry path resolves within the target
// directory, preventing zip-slip (path traversal) attacks.
func sanitizeTarPath(targetDir, headerName string) (string, error) {
	cleanPath := filepath.Join(targetDir, headerName)
	if !strings.HasPrefix(filepath.Clean(cleanPath), filepath.Clean(targetDir)+string(os.PathSeparator)) {
		return "", fmt.Errorf("tar entry %q resolves to %q which is outside target directory %q", headerName, cleanPath, targetDir)
	}
	return cleanPath, nil
}
