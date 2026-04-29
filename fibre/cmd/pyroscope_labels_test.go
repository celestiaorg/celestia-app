package main

import (
	"bytes"
	"testing"

	"github.com/google/pprof/profile"
	"github.com/grafana/pyroscope-go/upstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureUpstream struct {
	last    *upstream.UploadJob
	flushed int
}

func (c *captureUpstream) Upload(j *upstream.UploadJob) { c.last = j }
func (c *captureUpstream) Flush()                       { c.flushed++ }

func TestStripLabelsUpstream_RemovesAllSampleLabels(t *testing.T) {
	p := &profile.Profile{
		SampleType: []*profile.ValueType{{Type: "cpu", Unit: "samples"}},
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     1,
		Sample: []*profile.Sample{{
			Value: []int64{1},
			Label: map[string][]string{
				"output-level": {"L6"},
				"pebble":       {"compact"},
				"other_valid":  {"keep-me"},
			},
			NumLabel: map[string][]int64{"size": {42}},
			NumUnit:  map[string][]string{"size": {"bytes"}},
		}},
	}
	var buf bytes.Buffer
	require.NoError(t, p.Write(&buf))

	cap := &captureUpstream{}
	s := &stripLabelsUpstream{inner: cap}
	s.Upload(&upstream.UploadJob{Profile: buf.Bytes()})

	require.NotNil(t, cap.last, "inner Upload must be invoked")
	out, err := profile.ParseData(cap.last.Profile)
	require.NoError(t, err)
	require.Len(t, out.Sample, 1)
	assert.Empty(t, out.Sample[0].Label, "all string labels must be stripped")
	assert.Empty(t, out.Sample[0].NumLabel, "all numeric labels must be stripped")
	assert.Empty(t, out.Sample[0].NumUnit, "all numeric label units must be stripped")
}

func TestStripLabelsUpstream_NoLabelsPassesThroughUnchanged(t *testing.T) {
	p := &profile.Profile{
		SampleType: []*profile.ValueType{{Type: "cpu", Unit: "samples"}},
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     1,
		Sample:     []*profile.Sample{{Value: []int64{1}}},
	}
	var buf bytes.Buffer
	require.NoError(t, p.Write(&buf))
	raw := buf.Bytes()

	cap := &captureUpstream{}
	s := &stripLabelsUpstream{inner: cap}
	s.Upload(&upstream.UploadJob{Profile: raw})

	require.NotNil(t, cap.last)
	// When no labels need stripping we avoid re-encoding — the caller
	// sees the exact bytes they passed in.
	assert.Equal(t, raw, cap.last.Profile)
}

func TestStripLabelsUpstream_MalformedProfilePassesThrough(t *testing.T) {
	raw := []byte("definitely not a pprof profile")
	cap := &captureUpstream{}
	s := &stripLabelsUpstream{inner: cap}
	s.Upload(&upstream.UploadJob{Profile: raw})
	require.NotNil(t, cap.last)
	assert.Equal(t, raw, cap.last.Profile, "malformed profile must forward unchanged")
}

func TestStripLabelsUpstream_FlushForwards(t *testing.T) {
	cap := &captureUpstream{}
	s := &stripLabelsUpstream{inner: cap}
	s.Flush()
	s.Flush()
	assert.Equal(t, 2, cap.flushed)
}
