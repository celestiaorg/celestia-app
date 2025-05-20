package appd

import "io"

// TODO: ConfigOption never appears to be used. Delete it.
type ConfigOption func(*Appd) *Appd

// WithStdOut configures the standard output of the app.
func WithStdOut(stdout io.Writer) ConfigOption {
	return func(a *Appd) *Appd {
		a.stdout = stdout
		return a
	}
}

// WithStdErr configures the standard error of the app.
func WithStdErr(stderr io.Writer) ConfigOption {
	return func(a *Appd) *Appd {
		a.stderr = stderr
		return a
	}
}

// WithStdIn configures the standard input of the app.
func WithStdIn(stdin io.Reader) ConfigOption {
	return func(a *Appd) *Appd {
		a.stdin = stdin
		return a
	}
}
