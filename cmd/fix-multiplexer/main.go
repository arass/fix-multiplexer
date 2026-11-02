package main

import (
	"flag"
	"fmt"
	"os"

	"fix-multiplexer/internal/config"
)

func main() {
	path := flag.String("config", "config/example.yaml", "YAML configuration file")
	check := flag.Bool("check-config", false, "validate configuration and exit")
	flag.Parse()
	f, err := os.Open(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open configuration")
		os.Exit(1)
	}
	_, err = config.Load(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check {
		fmt.Println("configuration valid")
		return
	}
	fmt.Fprintln(os.Stderr, "session runtime not implemented yet; use -check-config")
	os.Exit(1)
}
