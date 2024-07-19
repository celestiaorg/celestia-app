package main

// NoopAppOptions is a no-op implementation of servertypes.AppOptions.
type NoopAppOptions struct{}

func (nao NoopAppOptions) Get(string) interface{} {
	return nil
}
