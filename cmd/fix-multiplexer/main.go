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
)

func main() {
	path := flag.String("config", "config/example.yaml", "YAML configuration file")
	check := flag.Bool("check-config", false, "validate configuration and exit")
	demoFlag := flag.Bool("demo", false, "run a synthetic local three-client FIX smoke scenario")
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
