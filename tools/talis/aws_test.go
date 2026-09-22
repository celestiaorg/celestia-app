package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAWSArchitecture(t *testing.T) {
	tests := map[string]string{
		"c8gn.24xlarge": "arm64",
		"c8gn.48xlarge": "arm64",
		"m7g.large":     "arm64",
		"c7gd.xlarge":   "arm64",
		"c6in.4xlarge":  "x86_64",
		"c6in.32xlarge": "x86_64",
		"t3.medium":     "x86_64",
		"m7i.large":     "x86_64",
	}
	for slug, want := range tests {
		t.Run(slug, func(t *testing.T) {
			require.Equal(t, want, awsArchitecture(slug))
		})
	}
}

func TestAWSUbuntuImageNamePattern(t *testing.T) {
	require.Equal(t, "ubuntu/images/hvm-ssd*/ubuntu-noble-24.04-amd64-server-*", awsUbuntuImageNamePattern("x86_64"))
	require.Equal(t, "ubuntu/images/hvm-ssd*/ubuntu-noble-24.04-arm64-server-*", awsUbuntuImageNamePattern("arm64"))
}

func TestAWSIamInstanceProfile(t *testing.T) {
	require.Nil(t, awsIamInstanceProfile(""))
	got := awsIamInstanceProfile("fibre-validator")
	require.NotNil(t, got)
	require.Equal(t, "fibre-validator", *got.Name)
}
