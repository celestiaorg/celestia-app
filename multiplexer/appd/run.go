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

const AppdStopped = -1

// Appd represents a celestia-appd binary.
type Appd struct {
	pid    int
	path   string
	stdin  io.Reader
	stderr io.Writer
	stdout io.Writer
}

// New returns a new Appd instance.
func New(version string, binary []byte, options ...ConfigOption) (*Appd, error) {
	if len(binary) == 0 {
		return nil, fmt.Errorf("no binary data available: ensure `binary` is not empty")
	}

	// untar the binary.
	gzr, err := gzip.NewReader(bytes.NewReader(binary))
	if err != nil {
		return nil, fmt.Errorf("failed to read binary data for %s: %w", version, err)
	}
	defer gzr.Close()

	// create a temporary directory for extraction
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("celestia-appd-%s-", version))
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// extract all files from the tar archive to the temp directory
	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %w", err)
		}

		if header.FileInfo().IsDir() {
			// Create directory
			dirPath := filepath.Join(tmpDir, header.Name)
			if err := os.MkdirAll(dirPath, 0o755); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", dirPath, err)
			}
			continue
		}

		// Create file path
		filePath := filepath.Join(tmpDir, header.Name)

		// Create parent directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create parent directory for %s: %w", filePath, err)
		}

		// Create file
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, header.FileInfo().Mode())
		if err != nil {
			return nil, fmt.Errorf("failed to create file %s: %w", filePath, err)
		}

		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to copy file contents to %s: %w", filePath, err)
		}
		f.Close()
	}

	// look for the executable binary in the extracted files
	var binaryPath string
	err = filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
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
		return nil, fmt.Errorf("failed to find executable binary in the archive: %w", err)
	} else if binaryPath == "" {
		return nil, fmt.Errorf("no executable binary found in the archive for %s", version)
	}

	appd := &Appd{
		path:   binaryPath,
		pid:    AppdStopped, // initialize with stopped state
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}

	for _, option := range options {
		option(appd)
	}

	// Verify the binary is executable for the current architecture
	testCmd := exec.Command(binaryPath, "--help")
	testOutput, err := testCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("binary validation failed (%s): %w\nOutput: %s", binaryPath, err, string(testOutput))
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

// CreateExecCommand creates an exec.Cmd for the appd binary.
func (a *Appd) CreateExecCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(a.path, args...)
	cmd.Stdin = a.stdin
	cmd.Stdout = a.stdout
	cmd.Stderr = a.stderr
	return cmd
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
