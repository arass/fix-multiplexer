package fixadapter

import (
	"errors"
	"os"
	"path/filepath"

	"fix-multiplexer/internal/config"
	"fix-multiplexer/internal/fileoutput"
)

func (r *Runtime) openFileOutputs(c config.Config) error {
	if len(c.Outputs) == 0 {
		return nil
	}
	// Resolve actual filesystem paths after store roots exist, before acquiring
	// any output lease or deleting rotated captures. This catches ancestor links.
	var occupied []string
	for _, directory := range []string{c.Upstream.StoreDir, c.Downstream.StoreDir} {
		path, err := filepath.EvalSymlinks(directory)
		if err != nil {
			return errors.New("cannot resolve store root for file outputs")
		}
		occupied = append(occupied, path)
	}
	for _, output := range c.Outputs {
		if err := os.MkdirAll(output.Directory, 0700); err != nil {
			return errors.New("cannot create file output directory")
		}
		path, err := filepath.EvalSymlinks(output.Directory)
		if err != nil {
			return errors.New("cannot resolve file output directory")
		}
		for _, other := range occupied {
			if config.PathsOverlap(path, other) {
				return errors.New("file outputs must be separate from session stores and other outputs")
			}
		}
		occupied = append(occupied, path)
	}
	outputs, err := fileoutput.New(c.Outputs, c.Fanout.AppTypes, c.Fanout.DenyTypes, fileoutput.Options{OnFailure: r.Observer.OutputFailure})
	if err != nil {
		return err
	}
	r.FileOutputs = outputs
	r.Application.FileOutputs = outputs
	return nil
}
