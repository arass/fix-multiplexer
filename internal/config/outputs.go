package config

import (
	"errors"
	"path/filepath"
	"strings"
)

type Output struct {
	Name                string   `yaml:"name"`
	Type                string   `yaml:"type"`
	Directory           string   `yaml:"directory"`
	Format              string   `yaml:"format"`
	ReceiveMessageTypes []string `yaml:"receive_message_types"`
	QueueMessages       int      `yaml:"queue_messages"`
	QueueBytes          int64    `yaml:"queue_bytes"`
	MaxFileBytes        int64    `yaml:"max_file_bytes"`
	MaxFiles            int      `yaml:"max_files"`
}

// Zero values select defaults for file limits, but never change an explicit
// empty receive list into an inherited subscription.
func (o Output) WithDefaults() Output {
	if o.Format == "" {
		o.Format = "jsonl"
	}
	if o.QueueMessages == 0 {
		o.QueueMessages = 1000
	}
	if o.QueueBytes == 0 {
		o.QueueBytes = 8 << 20
	}
	if o.MaxFileBytes == 0 {
		o.MaxFileBytes = 64 << 20
	}
	if o.MaxFiles == 0 {
		o.MaxFiles = 16
	}
	return o
}

func (o Output) MarshalYAML() (any, error) {
	fields := map[string]any{
		"name": o.Name, "type": o.Type, "directory": o.Directory, "format": o.Format,
		"queue_messages": o.QueueMessages, "queue_bytes": o.QueueBytes,
		"max_file_bytes": o.MaxFileBytes, "max_files": o.MaxFiles,
	}
	if o.ReceiveMessageTypes != nil {
		fields["receive_message_types"] = o.ReceiveMessageTypes
	}
	return fields, nil
}

// PathsOverlap compares absolute lexical paths, including ancestor relationships.
// Runtime additionally checks resolved paths to account for symlinks/junctions.
func PathsOverlap(a, b string) bool {
	a, e1 := filepath.Abs(a)
	b, e2 := filepath.Abs(b)
	if e1 != nil || e2 != nil {
		return true
	}
	inside := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return inside(a, b) || inside(b, a)
}

func (c Config) validateOutputs() error {
	bad := errors.New("invalid configuration: outputs require unique names, separate directories, file/jsonl format, valid receive subsets and bounded queues/files")
	if len(c.Outputs) > 8 {
		return bad
	}
	names := make(map[string]bool)
	var directories []string
	var memory, disk int64
	for _, raw := range c.Outputs {
		o := raw.WithDefaults()
		if !identifier.MatchString(o.Name) || names[o.Name] || o.Type != "file" || o.Format != "jsonl" || o.Directory == "" {
			return bad
		}
		names[o.Name] = true
		if o.QueueMessages < 1 || o.QueueMessages > 4096 || o.QueueBytes < 4096 || o.QueueBytes > 64<<20 || o.MaxFileBytes < 4096 || o.MaxFileBytes > 1<<30 || o.MaxFiles < 1 || o.MaxFiles > 128 {
			return bad
		}
		memory += o.QueueBytes
		disk += o.MaxFileBytes * int64(o.MaxFiles)
		if memory > 64<<20 || disk > 8<<30 {
			return bad
		}
		for _, other := range append(append([]string{}, directories...), c.Upstream.StoreDir, c.Downstream.StoreDir) {
			if PathsOverlap(o.Directory, other) {
				return bad
			}
		}
		directories = append(directories, o.Directory)
		seen := make(map[string]bool)
		for _, typ := range o.ReceiveMessageTypes {
			if seen[typ] || IsAdmin(typ) {
				return bad
			}
			seen[typ] = true
			found := false
			for _, allowed := range c.Fanout.AppTypes {
				if typ == allowed {
					found = true
					break
				}
			}
			if !found {
				return bad
			}
		}
	}
	return nil
}
