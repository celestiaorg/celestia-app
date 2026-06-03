package main

import (
	"bytes"

	"github.com/google/pprof/profile"
	"github.com/grafana/pyroscope-go/upstream"
)

// stripLabelsUpstream wraps a pyroscope-go Upstream and removes every
// runtime/pprof sample label before upload.
//
// Why: Pyroscope's ingest API enforces the Prometheus label-name rule
// `[a-zA-Z_][a-zA-Z0-9_]*` and rejects the entire profile (HTTP 422) if
// any sample carries a label key that violates it. Pebble — which fibre
// uses as its shard store — attaches pprof labels like
// `output-level="L6"` and `pebble="compact"` to its background
// goroutines via `pprof.Do()`. CPU profiles sample at 100 Hz and almost
// always catch those goroutines in flight, so every fibre CPU profile
// gets dropped. Memory / alloc profiles rarely sample under those
// contexts and go through, which is why flamegraphs for the fibre
// server show heap but not CPU.
//
// Stripping sample labels is the minimum fix. Pyroscope's flamegraph UI
// attributes CPU samples by stack frames — the frames already identify
// pebble flush/compact work. Labels attached via `pprof.Do` only show
// up as a secondary filter dimension that nobody uses for this. The
// service-level tags (hostname, version, app_name) come from
// SessionConfig.Tags, not per-sample labels, so they survive intact.
//
// On any parse failure we forward the original bytes unchanged:
// profiling must degrade gracefully, never take the service down.
type stripLabelsUpstream struct {
	inner upstream.Upstream
}

func (s *stripLabelsUpstream) Upload(job *upstream.UploadJob) {
	p, err := profile.ParseData(job.Profile)
	if err != nil {
		s.inner.Upload(job)
		return
	}
	dirty := false
	for _, sam := range p.Sample {
		if len(sam.Label) > 0 || len(sam.NumLabel) > 0 || len(sam.NumUnit) > 0 {
			sam.Label = nil
			sam.NumLabel = nil
			sam.NumUnit = nil
			dirty = true
		}
	}
	if dirty {
		var buf bytes.Buffer
		if err := p.Write(&buf); err == nil {
			job.Profile = buf.Bytes()
		}
	}
	s.inner.Upload(job)
}

func (s *stripLabelsUpstream) Flush() { s.inner.Flush() }
