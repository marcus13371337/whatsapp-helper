package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
	"github.com/marcus13371337/whatsapp-helper/internal/wa"
)

var version = "dev"

func main() {
	stateDir := flag.String("state-dir", "", "keep the session in this folder between runs, for running the helper by hand")
	showVersion := flag.Bool("version", false, "print the helper and protocol versions")
	flag.Parse()
	if *showVersion {
		fmt.Printf("whatsapp-helper %s (protocol %d)\n", version, helper.ProtocolVersion)
		return
	}
	if *stateDir != "" {
		if err := os.MkdirAll(*stateDir, 0o700); err != nil {
			fmt.Fprintln(os.Stderr, "could not create the state folder:", err)
			os.Exit(1)
		}
	}

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = zerolog.New(wa.QuietLog(os.Stderr)).With().Timestamp().Logger()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	err := helper.Run(ctx, helper.Options{
		In:       os.Stdin,
		Out:      os.Stdout,
		Log:      os.Stderr,
		Version:  version,
		Connect:  wa.Connector(os.Stderr),
		StateDir: *stateDir,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "shutdown problem:", err)
		os.Exit(1)
	}
}
