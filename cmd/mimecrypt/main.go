package main

import (
	"context"
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
		log.Fatalf("%v", err)
	}
}
