package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/celestiaorg/celestia-app/v6/metrics/server"
)

func main() {
	port := flag.Int("port", 9900, "gRPC server port")
	targetsFile := flag.String("targets-file", "/data/targets.json", "Path to write Prometheus file_sd targets")
	flag.Parse()

	srv := server.NewServer(*port, *targetsFile)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("shutting down...")
		srv.Stop()
	}()

	if err := srv.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
