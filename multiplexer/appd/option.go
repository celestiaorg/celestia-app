package appd

import "io"

type CfgOption func(*Appd) *Appd

// WithStdOut configures the standard output of the app.
func WithStdOut(stdout io.Writer) CfgOption {
	return func(a *Appd) *Appd {
		a.stdout = stdout
		return a
	}
}

// WithStdErr configures the standard error of the app.
func WithStdErr(stderr io.Writer) CfgOption {
	return func(a *Appd) *Appd {
		a.stderr = stderr
		return a
	}
}

// WithStdIn configures the standard input of the app.
func WithStdIn(stdin io.Reader) CfgOption {
	return func(a *Appd) *Appd {
		a.stdin = stdin
		return a
	}
}
