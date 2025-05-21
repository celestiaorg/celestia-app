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
	// celestiaAppTempBinaryDir is the directory where uncompressed celestia-app binaries are stored.
	celestiaAppTempBinaryDir = "/tmp/celestia-app"
)

const AppdStopped = -1

// Appd represents a celestia-appd binary.
type Appd struct {
	// version is the version of the celestia-appd binary.
	// Example: "v3.10.0-arabica"
	version string
	// pid is the process ID of the celestia-appd binary.
	pid int
	// pathToBinary is the pathToBinary to the celestia-appd binary.
	pathToBinary string
	stdin        io.Reader
	stderr       io.Writer
	stdout       io.Writer
}

// New returns a new Appd instance.
func New(version string, binary []byte) (*Appd, error) {
	if version == "" {
		return nil, fmt.Errorf("version is required")
	}

	if len(binary) == 0 {
		return nil, fmt.Errorf("no binary data available: ensure binary is not empty")
	}

	if !isBinaryAlreadyExtracted(version) {
		err := extractBinary(version, binary)
		if err != nil {
			return nil, fmt.Errorf("failed to extract binary: %w", err)
		}
	}

	pathToBinary, err := getPathToBinary(version, celestiaAppTempBinaryDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get path to binary: %w", err)
	}

	err = verifyBinaryIsExecutable(pathToBinary)
	if err != nil {
		fmt.Printf("failed to verify binary is executable: %s\n", err)
	}

	appd := &Appd{
		version:      version,
		pid:          AppdStopped,
		pathToBinary: pathToBinary,
		stdin:        os.Stdin,
		stdout:       os.Stdout,
		stderr:       os.Stderr,
	}
	return appd, nil
}

// Start starts the appd binary with the given arguments.
func (a *Appd) Start(args ...string) error {
	cmd := exec.Command(a.pathToBinary, append([]string{"start"}, args...)...)

	// Set up I/O
	cmd.Stdin = a.stdin
	cmd.Stdout = a.stdout
	cmd.Stderr = a.stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", a.pathToBinary, err)
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
	cmd := exec.Command(a.pathToBinary, args...)
	cmd.Stdin = a.stdin
	cmd.Stdout = a.stdout
	cmd.Stderr = a.stderr
	return cmd
}

// getPathToBinary returns the path to the celestia-appd binary in the
// baseDirectory.
func getPathToBinary(version string, baseDirectory string) (binaryPath string, err error) {
	// look for the executable binary in the extracted files
	err = filepath.Walk(baseDirectory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && info.Mode()&0o111 != 0 {
			binaryPath = path
			return filepath.SkipAll // Found it, stop searching
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to find executable binary in the archive: %w", err)
	}
	if binaryPath == "" {
		return "", fmt.Errorf("no executable binary found in the archive for %s", version)
	}
	return binaryPath, nil
}

func extractBinary(version string, binary []byte) error {
	// untar the binary.
	gzipReader, err := gzip.NewReader(bytes.NewReader(binary))
	if err != nil {
		return fmt.Errorf("failed to read binary data for %s: %w", version, err)
	}
	defer gzipReader.Close()

	directoryForVersion := getDirectoryForVersion(version)
	fmt.Printf("Creating directory: %s\n", directoryForVersion)
	if err := os.MkdirAll(directoryForVersion, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	fmt.Printf("Directory created: %s\n", directoryForVersion)

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
			dirPath := filepath.Join(directoryForVersion, header.Name)
			if err := os.MkdirAll(dirPath, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dirPath, err)
			}
			continue
		}

		// Create file path
		filePath := filepath.Join(directoryForVersion, header.Name)

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

// isBinaryAlreadyExtracted returns true if the binary for the given version has already been extracted.
func isBinaryAlreadyExtracted(version string) bool {
	directoryForVersion := getDirectoryForVersion(version)
	_, err := os.Stat(directoryForVersion)
	if err != nil {
		return false
	}
	return true
}

// getDirectoryForVersion returns the directory for the given version.
func getDirectoryForVersion(version string) string {
	return fmt.Sprintf("%s/%s", celestiaAppTempBinaryDir, version)
}

func verifyBinaryIsExecutable(pathToBinary string) error {
	testCmd := exec.Command(pathToBinary, "--help")
	testOutput, err := testCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("binary validation failed (%s): %w\nOutput: %s", pathToBinary, err, string(testOutput))
	}
	return nil
}
