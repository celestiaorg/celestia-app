package appd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Appd represents a celestia-appd binary.
type Appd struct {
	// version is the version of the celestia-appd binary.
	// Example: "v9.0.4"
	version string
	// compressedBinary is the gzipped tarball containing the binary.
	compressedBinary []byte
	// path is the path to the celestia-appd binary. It is set once the binary
	// is extracted.
	path   string
	stdin  io.Reader
	stderr io.Writer
	stdout io.Writer
	// cmd is the started celestia-appd binary.
	cmd *exec.Cmd
	// done is closed once the process started by Start has exited. It is
	// created by Start so that a single background goroutine owns cmd.Wait()
	// (exec.Cmd forbids calling Wait more than once); Stop and IsStopped
	// observe the exit through done instead of calling Wait themselves.
	done chan struct{}
	// exitErr holds the result of cmd.Wait(). It is only safe to read after
	// done is closed.
	exitErr error
	// exited delivers the result of cmd.Wait() (nil on a clean exit) once the
	// process exits, and is then closed. See Exited.
	exited chan error
}

// New returns a new Appd instance.
func New(version string, compressedBinary []byte) (*Appd, error) {
	if len(compressedBinary) == 0 {
		return nil, fmt.Errorf("no compressed binary available for version %s", version)
	}

	appd := &Appd{
		version:          version,
		compressedBinary: compressedBinary,
		stdin:            os.Stdin,
		stdout:           os.Stdout,
		stderr:           os.Stderr,
	}
	return appd, nil
}

// ensureExtracted extracts the binary on first use and sets its path.
func (a *Appd) ensureExtracted() error {
	if a.path != "" {
		return nil
	}
	dir := getDirectoryForVersion(a.version)
	if err := ensureBinaryDecompressed(a.version, a.compressedBinary); err != nil {
		return fmt.Errorf("failed to decompress binary to %s: %w", dir, err)
	}
	pathToBinary, err := getPathToBinary(a.version)
	if err != nil {
		return fmt.Errorf("failed to get path to binary in %s: %w", dir, err)
	}
	a.path = pathToBinary
	return nil
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
	if err := a.ensureExtracted(); err != nil {
		return err
	}
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
	a.cmd = cmd

	// A single background goroutine owns cmd.Wait(): exec.Cmd forbids calling
	// Wait more than once, so Stop and IsStopped observe the exit via done and
	// the multiplexer's watcher receives it via exited.
	done := make(chan struct{})
	exited := make(chan error, 1)
	a.done, a.exited = done, exited
	go func() {
		err := cmd.Wait()
		a.exitErr = err
		close(done)
		exited <- err
		close(exited)
	}()
	return nil
}

// Exited returns a channel that delivers the result of the process's exit
// (the error returned by cmd.Wait, nil on a clean exit) and is closed
// afterwards. The multiplexer uses it to detect an embedded app that exits
// unexpectedly. If Start has not been called, the returned channel is nil and
// never delivers.
func (a *Appd) Exited() <-chan error {
	return a.exited
}

func (a *Appd) IsRunning() bool {
	return !a.IsStopped()
}

func (a *Appd) IsStopped() bool {
	// Never started or failed to start.
	if a.cmd == nil || a.cmd.Process == nil || a.done == nil {
		return true
	}

	// done is closed once the process has exited.
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

// Stop interrupts and then kills the running appd process if it exists and
// waits for it to fully exit. If the process is not running, it returns nil.
// The method will wait up to 6 seconds for graceful shutdown before force killing.
func (a *Appd) Stop() error {
	if a.cmd == nil {
		return nil
	}
	if a.cmd.Process == nil {
		return nil
	}

	// The Wait goroutine reaps the child as soon as it exits, so signalling a
	// child that already exited returns os.ErrProcessDone. It is already
	// stopped, which is what the caller asked for, so fall through to done.
	err := a.cmd.Process.Signal(os.Interrupt)
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		log.Printf("Failed to send interrupt signal, attempting to kill: %v", err)
		if err := a.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("failed to kill process with PID %d: %w", a.cmd.Process.Pid, err)
		}

		<-a.done
		if a.exitErr != nil {
			log.Printf("Process finished with error: %v\n", a.exitErr)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	select {
	case <-a.done:
		if a.exitErr != nil {
			log.Printf("Process finished with error: %v\n", a.exitErr)
		} else {
			log.Printf("Process finished with no error\n")
		}
		return nil
	case <-ctx.Done():
		log.Printf("Process did not exit within 6 seconds, force killing")
		if err := a.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("failed to kill process with PID %d after timeout: %w", a.cmd.Process.Pid, err)
		}

		<-a.done
		if a.exitErr != nil {
			log.Printf("Process finished with error after force kill: %v\n", a.exitErr)
		} else {
			log.Printf("Process finished after force kill\n")
		}
		return nil
	}
}

// CreateExecCommand creates an exec.Cmd for the appd binary.
func (a *Appd) CreateExecCommand(args ...string) (*exec.Cmd, error) {
	if err := a.ensureExtracted(); err != nil {
		return nil, err
	}
	cmd := exec.Command(a.path, args...)
	cmd.Stdin = a.stdin
	cmd.Stdout = a.stdout
	cmd.Stderr = a.stderr
	return cmd, nil
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
	if err := os.MkdirAll(filepath.Dir(targetDirectory), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Extract into a staging directory and only publish it once every file has
	// been written and closed. Otherwise a failed extraction would leave the
	// version directory in place and isBinaryDecompressed would report the
	// unusable output as a completed extraction on the next start.
	stagingDirectory, err := os.MkdirTemp(filepath.Dir(targetDirectory), "."+filepath.Base(targetDirectory)+".tmp-")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDirectory)

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
			dirPath, err := sanitizeTarPath(stagingDirectory, header.Name)
			if err != nil {
				return fmt.Errorf("path traversal in tar entry: %w", err)
			}
			if err := os.MkdirAll(dirPath, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dirPath, err)
			}
			continue
		}

		// Create file path
		filePath, err := sanitizeTarPath(stagingDirectory, header.Name)
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

		_, copyErr := io.Copy(f, tarReader)
		if copyErr != nil {
			copyErr = fmt.Errorf("failed to copy file contents to %s: %w", filePath, copyErr)
		}
		closeErr := f.Close()
		if closeErr != nil {
			closeErr = fmt.Errorf("failed to close file %s: %w", filePath, closeErr)
		}
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
	}

	if err := os.Rename(stagingDirectory, targetDirectory); err != nil {
		// Another instance sharing this node home may have published the same
		// version first. Its directory is a complete extraction, so accept it
		// rather than failing the caller.
		if isBinaryDecompressed(version) {
			return nil
		}
		return fmt.Errorf("failed to publish extracted binary for %s: %w", version, err)
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
