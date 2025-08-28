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
	// cmd is the started celestia-appd binary.
	cmd *exec.Cmd
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

// Start starts the appd binary with the given arguments.
func (a *Appd) Start(args ...string) error {
	cmd := exec.Command(a.path, append([]string{"start"}, args...)...)

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
	return nil
}

func (a *Appd) IsRunning() bool {
	return !a.IsStopped()
}

func (a *Appd) IsStopped() bool {
	// Never started or failed to start
	if a.cmd == nil || a.cmd.Process == nil {
		return true
	}

	// If ProcessState is not nil, it means Wait() was called and the process has finished
	// (either by exiting normally or being terminated by a signal)
	if a.cmd.ProcessState != nil {
		return true
	}

	// ProcessState is nil, which means the process is still running
	return false
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

	err := a.cmd.Process.Signal(os.Interrupt)
	if err != nil {
		log.Printf("Failed to send interrupt signal, attempting to kill: %v", err)
		if err := a.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process with PID %d: %w", a.cmd.Process.Pid, err)
		}

		if err := a.cmd.Wait(); err != nil {
			log.Printf("Process finished with error: %v\n", err)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- a.cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			log.Printf("Process finished with error: %v\n", err)
		} else {
			log.Printf("Process finished with no error\n")
		}
		return nil
	case <-ctx.Done():
		log.Printf("Process did not exit within 6 seconds, force killing")
		if err := a.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process with PID %d after timeout: %w", a.cmd.Process.Pid, err)
		}

		if err := <-done; err != nil {
			log.Printf("Process finished with error after force kill: %v\n", err)
		} else {
			log.Printf("Process finished after force kill\n")
		}
		return nil
	}
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
			dirPath := filepath.Join(targetDirectory, header.Name)
			if err := os.MkdirAll(dirPath, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dirPath, err)
			}
			continue
		}

		// Create file path
		filePath := filepath.Join(targetDirectory, header.Name)

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
