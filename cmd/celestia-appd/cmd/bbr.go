package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"cosmossdk.io/log"
	"github.com/spf13/cobra"
)

const FlagForceNoBBR = "force-no-bbr"

const bbrWarning = `
The BBR (Bottleneck Bandwidth and Round-trip propagation time) congestion control algorithm is not enabled in this system's kernel.
BBR is important for the performance of the p2p stack.

To enable BBR (Linux only):
sudo modprobe tcp_bbr
sudo sysctl -w net.core.default_qdisc=fq
sudo sysctl -w net.ipv4.tcp_congestion_control=bbr

To persist across reboots, load the module at boot:
echo tcp_bbr | sudo tee /etc/modules-load.d/bbr.conf

and add these lines to /etc/sysctl.conf:
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr

Then verify BBR is enabled:
sysctl net.ipv4.tcp_congestion_control
or
cat /proc/sys/net/ipv4/tcp_congestion_control

This node will get worse p2p performance using a different congestion control algorithm.
If you need to bypass this check use the --force-no-bbr flag.
`

// checkBBR checks if BBR is enabled.
// It should be first run before RunE of the StartCmd.
func checkBBR(command *cobra.Command, logger log.Logger) error {
	forceNoBBR, err := command.Flags().GetBool(FlagForceNoBBR)
	if err != nil {
		return err
	}
	if forceNoBBR {
		return nil
	}

	// Only enforce BBR on Linux where /proc is available and BBR is supported
	if runtime.GOOS != "linux" {
		// Skip check silently for non-Linux OSes (e.g., macOS, Windows, BSD)
		return nil
	}

	file, err := os.ReadFile("/proc/sys/net/ipv4/tcp_congestion_control")
	if err != nil {
		logger.Warn(bbrWarning)
		return fmt.Errorf("failed to read file '/proc/sys/net/ipv4/tcp_congestion_control' %w", err)
	}

	if !strings.Contains(string(file), "bbr") {
		logger.Warn(bbrWarning)
		return fmt.Errorf("BBR not enabled because output %v does not contain 'bbr'", strings.TrimSpace(string(file)))
	}

	return nil
}
