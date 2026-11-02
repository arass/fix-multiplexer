//go:build integration

package demo

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"
)

func TestLocalFIXScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration")
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir("internal/demo")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err := Run(ctx, &out); err != nil {
		t.Fatal(err)
	}
	t.Log(out.String())
}
