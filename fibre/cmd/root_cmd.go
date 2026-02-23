package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

const (
	defaultHome = ".celestia-fibre"
	envHome     = "FIBRE_HOME"
	flagHome    = "home"
)

func defaultHomePath() string {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return defaultHome
	}
	return filepath.Join(userHome, defaultHome)
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "fibre",
		Short:        "Run the Fibre data availability server",
		SilenceUsage: true,
	}
	rootCmd.PersistentFlags().String(flagHome, defaultHomePath(), fmt.Sprintf("fibre home directory (or set %s)", envHome))

	if home, ok := os.LookupEnv(envHome); ok && home != "" {
		err := rootCmd.PersistentFlags().Lookup(flagHome).Value.Set(home)
		if err != nil {
			fmt.Printf("Error setting home directory from %s: %v\n", envHome, err)
			os.Exit(1)
		}
	}

	rootCmd.AddCommand(
		newStartCmd(startServer),
		newVersionCmd(),
	)

	return rootCmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print fibre version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("version: %s\n", version)
			fmt.Printf("commit: %s\n", commit)
			fmt.Printf("build date: %s\n", buildDate)
			fmt.Printf("system: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			fmt.Printf("go: %s\n", runtime.Version())
		},
	}
}
