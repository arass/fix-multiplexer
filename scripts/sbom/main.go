// Produces a linked-module inventory directly from a Go executable. No source
// paths, environment values, credentials or FIX data are included.
package main

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type module struct {
	Path     string `json:"path"`
	Version  string `json:"version"`
	Checksum string `json:"checksum,omitempty"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: sbom Go-executable")
		os.Exit(1)
	}
	info, err := buildinfo.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read Go build metadata")
		os.Exit(1)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read executable")
		os.Exit(1)
	}
	defer f.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		fmt.Fprintln(os.Stderr, "cannot hash executable")
		os.Exit(1)
	}
	result := struct {
		Format         string            `json:"format"`
		Project        string            `json:"project"`
		ProjectLicense string            `json:"project_license"`
		GoVersion      string            `json:"go_version"`
		SHA256         string            `json:"binary_sha256"`
		Platform       map[string]string `json:"platform"`
		Modules        []module          `json:"modules"`
	}{Format: "Go linked-module inventory v1", Project: "fix-multiplexer", ProjectLicense: "Apache-2.0", GoVersion: info.GoVersion, SHA256: hex.EncodeToString(hash.Sum(nil)), Platform: make(map[string]string)}
	for _, s := range info.Settings {
		if s.Key == "GOOS" || s.Key == "GOARCH" || s.Key == "CGO_ENABLED" {
			result.Platform[s.Key] = s.Value
		}
	}
	for _, m := range info.Deps {
		result.Modules = append(result.Modules, module{m.Path, m.Version, m.Sum})
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encoder.Encode(result) != nil {
		os.Exit(1)
	}
}
