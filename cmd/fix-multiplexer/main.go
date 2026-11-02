package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/demo"
	"fix-multiplexer/internal/fixadapter"
	"fix-multiplexer/internal/maintenance"
)

func main() {
	path := flag.String("config", "config/example.yaml", "YAML configuration file")
	check := flag.Bool("check-config", false, "validate configuration and exit")
	demoFlag := flag.Bool("demo", false, "run a synthetic local three-client FIX smoke scenario")
	list := flag.Bool("list-clients", false, "list configured client store state while gateway is stopped")
	prune := flag.Bool("prune-stale", false, "retire and prune expired offline clients while gateway is stopped")
	reset := flag.String("reset-client", "", "coordinate fresh sequence-1 state for this allowlisted client while stopped")
	dryRun := flag.Bool("dry-run", true, "preview maintenance without deleting or resetting state")
	flag.Parse()
	if *demoFlag {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := demo.Run(ctx, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	f, err := os.Open(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open configuration")
		os.Exit(1)
	}
	c, err := config.Load(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check {
		fmt.Println("configuration valid")
		return
	}
	operations := 0
	command := ""
	if *list {
		operations++
		command = "list"
	}
	if *prune {
		operations++
		command = "prune"
	}
	if *reset != "" {
		operations++
		command = "reset"
	}
	if operations > 1 {
		fmt.Fprintln(os.Stderr, "choose one maintenance operation")
		os.Exit(1)
	}
	if operations == 1 {
		if err = maintenance.Run(c, command, *reset, *dryRun, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	runtime, err := fixadapter.New(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err = runtime.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "nonproduction FIX gateway started: live-only fanout; upstream failure returns a gateway FIX error; local sends do not acknowledge venue acceptance")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	runtime.Stop()
}
