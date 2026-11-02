package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/demo"
	"fix-multiplexer/internal/fixadapter"
	"fix-multiplexer/internal/maintenance"
	"fix-multiplexer/internal/observability"
)

func main() {
	path := flag.String("config", "config/example.yaml", "YAML configuration file")
	check := flag.Bool("check-config", false, "validate configuration and exit")
	demoFlag := flag.Bool("demo", false, "run a synthetic local three-client FIX smoke scenario")
	list := flag.Bool("list-clients", false, "list configured client store state while gateway is stopped")
	prune := flag.Bool("prune-stale", false, "retire and prune expired offline clients while gateway is stopped")
	reset := flag.String("reset-client", "", "coordinate fresh sequence-1 state for this allowlisted client while stopped")
	dryRun := flag.Bool("dry-run", true, "preview maintenance without deleting or resetting state")
	probe := flag.String("probe", "", "query local live, ready or metrics endpoint and exit")
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
	if *probe != "" {
		if (*probe != "live" && *probe != "ready" && *probe != "metrics") || c.Service.HealthBind == "" {
			fmt.Fprintln(os.Stderr, "probe requires enabled management and live, ready or metrics")
			os.Exit(1)
		}
		client := http.Client{Timeout: 2 * time.Second}
		response, e := client.Get("http://" + c.Service.HealthBind + "/" + *probe)
		if e != nil {
			fmt.Fprintln(os.Stderr, "management probe failed")
			os.Exit(1)
		}
		defer response.Body.Close()
		_, _ = io.Copy(os.Stdout, io.LimitReader(response.Body, 1<<20))
		if response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
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
	observer := observability.New(os.Stderr)
	startup := time.AfterFunc(c.Service.StartupTimeout, func() {
		observer.Operational("startup deadline exceeded; inspect store leases before retrying")
		os.Exit(1)
	})
	runtime, err := fixadapter.New(c, observer)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err = runtime.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var server *http.Server
	if c.Service.HealthBind != "" {
		listener, e := net.Listen("tcp", c.Service.HealthBind)
		if e != nil {
			runtime.Stop()
			fmt.Fprintln(os.Stderr, "cannot bind management listener")
			os.Exit(1)
		}
		server = &http.Server{Handler: observability.Handler(runtime), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 4096}
		go func() {
			if e := server.Serve(listener); e != nil && e != http.ErrServerClosed {
				observer.EngineError("management")
			}
		}()
	}
	observer.Operational("nonproduction; live-only fanout; upstream failure returns gateway FIX error; local sends do not acknowledge venue acceptance")
	startup.Stop()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), c.Service.ShutdownTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Stop() }()
	select {
	case shutdownErr := <-done:
		if shutdownErr != nil {
			observer.Operational("durable shutdown failed; inspect store state before restart")
			if server != nil {
				server.Close()
			}
			os.Exit(1)
		}
	case <-shutdown.Done():
		observer.Operational("shutdown deadline exceeded; in-flight delivery uncertain; stale leases require operator recovery")
		if server != nil {
			server.Close()
		}
		os.Exit(1)
	}
	if server != nil {
		if err = server.Shutdown(shutdown); err != nil {
			server.Close()
		}
	}
	observer.Operational("stopped")
}
