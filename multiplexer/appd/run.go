package appd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	// AppdStopped is the process ID of the celestia-appd binary when it is not running.
	AppdStopped = -1
)

// Appd represents a celestia-appd binary.
type Appd struct {
	// version is the version of the celestia-appd binary.
	// Example: "v3.10.0-arabica"
	version string
	// pid is the process ID of the celestia-appd binary.
	pid int
	// path is the path to the celestia-appd binary.
	path   string
	stdin  io.Reader
	stderr io.Writer
	stdout io.Writer
}

// New returns a new Appd instance.
func New(version string, compressedBinary []byte) (*Appd, error) {
	if len(compressedBinary) == 0 {
		return nil, fmt.Errorf("no compressed binary available for version %s", version)
	}

	err := decompressBinary(version, compressedBinary)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress binary: %w", err)
	}

	pathToBinary, err := getPathToBinary(version)
	if err != nil {
		return nil, fmt.Errorf("failed to get path to binary: %w", err)
	}

	err = verifyBinaryIsExecutable(pathToBinary)
	if err != nil {
		return nil, fmt.Errorf("failed to verify binary is executable: %w", err)
	}

	appd := &Appd{
		version: version,
		pid:     AppdStopped,
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

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", a.path, err)
	}

	a.pid = cmd.Process.Pid
	go func() {
		// wait for process to finish
		if err := cmd.Wait(); err != nil {
			log.Printf("Process finished with error: %v\n", err)
		}

		a.pid = -1 // reset pid
	}()

	return nil
}

// Stop terminates the running appd process if it exists.
func (a *Appd) Stop() error {
	if a.pid == AppdStopped {
		return nil
	}

	process, err := os.FindProcess(a.pid)
	if err != nil {
		return fmt.Errorf("failed to find process with PID %d: %w", a.pid, err)
	}

	// send SIGTERM for graceful shutdown
	if err := process.Signal(os.Interrupt); err != nil {
		log.Printf("Failed to send interrupt signal, attempting to kill: %v", err)
		// if interrupt fails, try harder with Kill
		if err := process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process with PID %d: %w", a.pid, err)
		}
	}

	// Wait for the process to exit
	_, err = process.Wait()
	if err != nil {
		log.Printf("Error waiting for process to exit: %v", err)
	}

	a.pid = AppdStopped
	return nil
}

// Pid returns the process ID of the appd process.
func (a *Appd) Pid() int {
	return a.pid
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

func decompressBinary(version string, binary []byte) error {
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

func verifyBinaryIsExecutable(pathToBinary string) error {
	testCmd := exec.Command(pathToBinary, "--help")
	testOutput, err := testCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("binary validation failed (%s): %w\nOutput: %s", pathToBinary, err, string(testOutput))
	}
	return nil
}

// getDirectoryForCelestiaAppBinaries returns the directory where all
// decompressed celestia-app binaries are stored. One directory exists per
// version.
func getDirectoryForCelestiaAppBinaries() string {
	return filepath.Join(os.Getenv("HOME"), ".celestia-app", "bin")
}

// getDirectoryForVersion returns the directory for a particular version.
func getDirectoryForVersion(version string) string {
	return filepath.Join(getDirectoryForCelestiaAppBinaries(), version)
}
