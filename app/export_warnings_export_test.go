package app

import "io"

// SetExportWarnings redirects the export warnings to w and returns a function
// that restores them. Tests only.
func SetExportWarnings(w io.Writer) (restore func()) {
	old := exportWarnings
	exportWarnings = w
	return func() { exportWarnings = old }
}
