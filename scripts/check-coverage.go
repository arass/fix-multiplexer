// Run with: go run ./scripts/check-coverage.go coverage.out
// Enforces statement-weighted fast-test coverage for the required core packages.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: check-coverage profile")
		os.Exit(1)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open coverage profile")
		os.Exit(1)
	}
	defer f.Close()
	type count struct{ total, covered int64 }
	totals := map[string]count{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			fmt.Fprintln(os.Stderr, "invalid coverage profile")
			os.Exit(1)
		}
		n, e1 := strconv.ParseInt(fields[1], 10, 64)
		hits, e2 := strconv.ParseInt(fields[2], 10, 64)
		if e1 != nil || e2 != nil || n < 0 || hits < 0 {
			fmt.Fprintln(os.Stderr, "invalid coverage counts")
			os.Exit(1)
		}
		slash := strings.LastIndex(fields[0], "/")
		if slash < 0 {
			continue
		}
		pkg := fields[0][:slash]
		v := totals[pkg]
		v.total += n
		if hits > 0 {
			v.covered += n
		}
		totals[pkg] = v
	}
	if scanner.Err() != nil {
		fmt.Fprintln(os.Stderr, "cannot read coverage profile")
		os.Exit(1)
	}
	failed := false
	for _, name := range []string{"config", "gateway", "fixadapter", "sessions", "retention", "fileoutput"} {
		v := totals["fix-multiplexer/internal/"+name]
		if v.total == 0 || v.covered*100 < v.total*90 {
			fmt.Fprintf(os.Stderr, "coverage below 90%% or missing: %s\n", name)
			failed = true
		} else {
			fmt.Printf("%s: %.1f%%\n", name, 100*float64(v.covered)/float64(v.total))
		}
	}
	if failed {
		os.Exit(1)
	}
}
