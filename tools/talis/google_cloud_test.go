package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGCDiskInitializeParams(t *testing.T) {
	tests := []struct {
		name               string
		slug               string
		expectHyperdisk    bool
		expectedThroughput int64
	}{
		{"validator gets full provisioning", GCDefaultValidatorMachineType, true, GCDefaultProvisionedThroughputMBs},
		{"encoder clamps to 8-vCPU throughput cap", GCDefaultEncoderMachineType, true, GCSmallShapeProvisionedThroughputMBs},
		{"reader clamps to 8-vCPU throughput cap", GCDefaultReaderMachineType, true, GCSmallShapeProvisionedThroughputMBs},
		{"unparsable vCPU count falls back to conservative cap", "c3d-standard-8-lssd", true, GCSmallShapeProvisionedThroughputMBs},
		{"observability keeps default disk type", GCDefaultObservabilityMachineType, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := NewBaseInstance(Validator)
			inst.Slug = tt.slug
			params := gcDiskInitializeParams(inst, "us-east1-b")

			require.Equal(t, GCDefaultImage, params.GetSourceImage())
			require.Equal(t, int64(GCDefaultDiskSizeGB), params.GetDiskSizeGb())

			if !tt.expectHyperdisk {
				require.Nil(t, params.DiskType)
				require.Nil(t, params.ProvisionedIops)
				require.Nil(t, params.ProvisionedThroughput)
				return
			}
			require.Equal(t, "zones/us-east1-b/diskTypes/"+GCDefaultDiskType, params.GetDiskType())
			require.Equal(t, GCDefaultProvisionedIops, params.GetProvisionedIops())
			require.Equal(t, tt.expectedThroughput, params.GetProvisionedThroughput())
		})
	}
}
