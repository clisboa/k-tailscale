package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"kobo-tailscale/helper"
)

func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return helper.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

func main() { os.Exit(run()) }
