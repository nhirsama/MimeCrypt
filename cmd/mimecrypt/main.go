package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"mimecrypt/internal/cli"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[mimecrypt] ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.ExecuteContext(ctx); err != nil {
		if shouldExitGracefully(err) {
			return
		}

		log.Printf("%v", err)
		os.Exit(1)
	}
}

func shouldExitGracefully(err error) bool {
	return errors.Is(err, context.Canceled)
}
