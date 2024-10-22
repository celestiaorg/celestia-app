package utils

import "io"

// NoopWriter implements the io.Writer interface.
var _ io.Writer = (*NoopWriter)(nil)

// NoopWriter is a no-op implementation of a writer.
type NoopWriter struct{}

func (nw *NoopWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}
