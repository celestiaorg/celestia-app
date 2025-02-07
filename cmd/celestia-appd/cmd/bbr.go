package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const FlagForceNoBBR = "force-no-bbr"

// checkBBR checks if BBR is enabled.
// It should be first run before RunE of the StartCmd.
func checkBBR(command *cobra.Command) error {
	const (
		warning = `
The BBR (Bottleneck Bandwidth and Round-trip propagation time) congestion control algorithm is not enabled in this system's kernel.
BBR is important for the performance of the p2p stack.

To enable BBR:
sudo modprobe tcp_bbr
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
sudo sysctl -p

Then verify BBR is enabled:
sysctl net.ipv4.tcp_congestion_control
or
cat /proc/sys/net/ipv4/tcp_congestion_control

This node will get worse p2p performance using a different congestion control algorithm.
If you need to bypass this check use the --force-no-bbr flag.
`
	)

	forceNoBBR, err := command.Flags().GetBool(FlagForceNoBBR)
	if err != nil {
		return err
	}
	if forceNoBBR {
		return nil
	}

	file, err := os.ReadFile("/proc/sys/net/ipv4/tcp_congestion_control")
	if err != nil {
		fmt.Print(warning)
		return fmt.Errorf("failed to read file '/proc/sys/net/ipv4/tcp_congestion_control' %w", err)
	}

	if !strings.Contains(string(file), "bbr") {
		fmt.Print(warning)
		return fmt.Errorf("BBR not enabled because output %v does not contain 'bbr'", string(file))
	}

	return nil
}
