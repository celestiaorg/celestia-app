package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_bbrWarning(t *testing.T) {
	assert.Contains(t, bbrWarning, "sudo sysctl -w net.core.default_qdisc=fq")
	assert.Contains(t, bbrWarning, "sudo sysctl -w net.ipv4.tcp_congestion_control=bbr")
	assert.Contains(t, bbrWarning, "/etc/sysctl.conf")
}
